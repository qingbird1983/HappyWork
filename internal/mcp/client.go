package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/happywork/happywork/internal/tool"
)

// Client owns one MCP server process.
type Client struct {
	name    string
	cfg     ServerConfig
	dataDir string
	log     *log.Logger

	mu       sync.Mutex
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	done     chan struct{}
	tools    []ToolInfo
	disabled bool
	restarts []time.Time

	writeMu sync.Mutex
	lifeMu  sync.Mutex
	callMu  sync.Mutex
	lines   chan *inbound
	nextID  atomic.Int64
	modern  bool
}

// NewClient builds a client; call Start to launch it.
func NewClient(name string, cfg ServerConfig, dataDir string, lg *log.Logger) *Client {
	if lg == nil {
		lg = log.Default()
	}
	return &Client{name: name, cfg: cfg, dataDir: dataDir, log: lg}
}

// Start launches the process, probes the protocol era and discovers tools.
func (c *Client) Start(ctx context.Context) error {
	c.lifeMu.Lock()
	defer c.lifeMu.Unlock()
	return c.startLocked(ctx)
}

func (c *Client) startLocked(ctx context.Context) error {
	c.mu.Lock()
	if c.disabled {
		c.mu.Unlock()
		return fmt.Errorf("mcp: server %q is disabled", c.name)
	}
	c.mu.Unlock()

	if err := c.launch(); err != nil {
		c.disable()
		return err
	}

	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	_, err := c.call(probeCtx, "server/discover", map[string]any{}, true)
	switch {
	case err == nil:
		c.mu.Lock()
		c.modern = true
		c.mu.Unlock()
	case isMethodNotFound(err):
		if ierr := c.initializeLegacy(ctx); ierr != nil {
			c.stop()
			c.disable()
			return ierr
		}
		c.mu.Lock()
		c.modern = false
		c.mu.Unlock()
	default:
		c.stop()
		c.disable()
		return fmt.Errorf("mcp: server %q probe: %w", c.name, err)
	}
	if err := c.discover(ctx); err != nil {
		c.stop()
		c.disable()
		return fmt.Errorf("mcp: server %q discovery: %w", c.name, err)
	}
	return nil
}

func (c *Client) initializeLegacy(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": legacyVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "HappyWork", "version": "0.2.0"},
	}
	initCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()
	res, err := c.call(initCtx, "initialize", params, false)
	if err != nil {
		return fmt.Errorf("mcp: legacy initialize: %w", err)
	}
	var body struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(res, &body); err != nil {
		return fmt.Errorf("mcp: legacy initialize response: %w", err)
	}
	if !legacyVersions[body.ProtocolVersion] {
		return fmt.Errorf("mcp: server %q returned unsupported protocol version %q", c.name, body.ProtocolVersion)
	}
	// Best effort: notification has no response.
	if err := c.writeMessage(rpcRequest{JSONRPC: "2.0", Method: "notifications/initialized", Params: map[string]any{}}); err != nil {
		return fmt.Errorf("mcp: initialized notification: %w", err)
	}
	return nil
}

func (c *Client) discover(ctx context.Context) error {
	cursor := ""
	var discovered []ToolInfo
	for page := 0; page < maxPages; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		c.mu.Lock()
		modern := c.modern
		c.mu.Unlock()
		res, err := c.call(ctx, "tools/list", params, modern)
		if err != nil {
			return err
		}
		var body struct {
			ResultType string `json:"resultType"`
			Tools      []struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				InputSchema json.RawMessage `json:"inputSchema"`
			} `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(res, &body); err != nil {
			return fmt.Errorf("tools/list: %w", err)
		}
		if modern && body.ResultType != "complete" {
			return fmt.Errorf("tools/list: resultType %q (want complete)", body.ResultType)
		}
		for _, t := range body.Tools {
			if len(discovered) >= maxTools {
				return fmt.Errorf("too many tools (limit %d)", maxTools)
			}
			info, err := c.buildToolInfo(t.Name, t.Description, t.InputSchema)
			if err != nil {
				c.log.Printf("mcp: server %q: skipping tool %q: %v", c.name, t.Name, err)
				continue
			}
			duplicate := false
			for _, existing := range discovered {
				if existing.ProviderName == info.ProviderName {
					c.log.Printf("mcp: server %q: skipping tool %q: provider name %q already used", c.name, t.Name, info.ProviderName)
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
			discovered = append(discovered, info)
		}
		if body.NextCursor == "" {
			c.mu.Lock()
			c.tools = discovered
			c.mu.Unlock()
			return nil
		}
		cursor = body.NextCursor
	}
	return fmt.Errorf("tools/list exceeds %d pages", maxPages)
}

func (c *Client) buildToolInfo(rawName, description string, schema json.RawMessage) (ToolInfo, error) {
	if rawName == "" || len(rawName) > 64 {
		return ToolInfo{}, fmt.Errorf("invalid tool name length")
	}
	if err := validateSchema(schema); err != nil {
		return ToolInfo{}, err
	}
	provider := "mcp__" + sanitizeName(c.name) + "__" + sanitizeName(rawName)
	if len(provider) > 64 {
		return ToolInfo{}, fmt.Errorf("provider tool name exceeds 64 chars")
	}
	policy := c.cfg.Tools[rawName]
	action := policy.Action
	if action == "" {
		action = "unknown"
	}
	// Check collisions within this server.
	c.mu.Lock()
	for _, existing := range c.tools {
		if existing.ProviderName == provider {
			c.mu.Unlock()
			return ToolInfo{}, fmt.Errorf("duplicate provider name %q", provider)
		}
	}
	c.mu.Unlock()
	return ToolInfo{
		RawName: rawName, ProviderName: provider, Description: description,
		Schema: schema, Action: action, Destructive: policy.Destructive,
	}, nil
}

func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func (c *Client) call(ctx context.Context, method string, params map[string]any, modern bool) (json.RawMessage, error) {
	c.callMu.Lock()
	defer c.callMu.Unlock()
	id := c.nextID.Add(1)
	if params == nil {
		params = map[string]any{}
	}
	if modern {
		params["_meta"] = map[string]any{
			"io.modelcontextprotocol/protocolVersion":    modernVersion,
			"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "HappyWork", "version": "0.2.0"},
			"io.modelcontextprotocol/clientCapabilities": map[string]any{},
		}
	}
	if err := c.writeMessage(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		return nil, err
	}
	c.mu.Lock()
	lines := c.lines
	c.mu.Unlock()
	if lines == nil {
		return nil, fmt.Errorf("mcp: server %q is not running", c.name)
	}
	for {
		select {
		case <-ctx.Done():
			_ = c.writeMessage(rpcRequest{JSONRPC: "2.0", Method: "notifications/cancelled",
				Params: map[string]any{"requestId": id, "reason": "cancelled or timed out"}})
			c.stop()
			return nil, ctx.Err()
		case msg, ok := <-lines:
			if !ok {
				c.mu.Lock()
				c.cmd = nil
				c.mu.Unlock()
				return nil, fmt.Errorf("mcp: server %q exited", c.name)
			}
			if !idMatches(msg.ID, id) {
				continue
			}
			if msg.Error != nil {
				return nil, msg.Error
			}
			return msg.Result, nil
		}
	}
}

func (c *Client) ensureRunning(ctx context.Context) error {
	c.lifeMu.Lock()
	defer c.lifeMu.Unlock()
	c.mu.Lock()
	alive := c.cmd != nil
	disabled := c.disabled
	c.mu.Unlock()
	if disabled {
		return fmt.Errorf("mcp: server %q is disabled", c.name)
	}
	if alive {
		return nil
	}
	c.mu.Lock()
	now := time.Now()
	kept := c.restarts[:0]
	for _, t := range c.restarts {
		if now.Sub(t) < restartWindow {
			kept = append(kept, t)
		}
	}
	if len(kept) >= maxRestarts {
		c.disabled = true
		c.restarts = kept
		c.mu.Unlock()
		return fmt.Errorf("mcp: server %q exceeded restart limit", c.name)
	}
	c.restarts = append(kept, now)
	c.mu.Unlock()
	c.stop()
	if err := c.startLocked(ctx); err != nil {
		c.disable()
		return err
	}
	return nil
}

// Tools returns the current tool snapshot as agent-facing tools.
func (c *Client) Tools() []tool.Tool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.disabled {
		return nil
	}
	out := make([]tool.Tool, 0, len(c.tools))
	for _, info := range c.tools {
		info := info
		out = append(out, &rpcTool{client: c, info: info})
	}
	return out
}

// CallTool invokes one tool with the server's timeout.
func (c *Client) CallTool(ctx context.Context, rawName string, args map[string]any) (map[string]any, error) {
	if err := c.ensureRunning(ctx); err != nil {
		return nil, err
	}
	timeout := time.Duration(c.cfg.Timeout()) * time.Second
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	c.mu.Lock()
	modern := c.modern
	c.mu.Unlock()
	res, err := c.call(callCtx, "tools/call", map[string]any{"name": rawName, "arguments": args}, modern)
	if err != nil {
		if callCtx.Err() != nil {
			c.stop()
		}
		return nil, err
	}
	return c.mapResult(res)
}

// mapResult converts MCP content into a model-safe payload.
func (c *Client) mapResult(raw json.RawMessage) (map[string]any, error) {
	var res struct {
		ResultType string `json:"resultType"`
		Content    []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Data     string `json:"data"`
			MimeType string `json:"mimeType"`
			URI      string `json:"uri"`
			Name     string `json:"name"`
		} `json:"content"`
		IsError           bool            `json:"isError"`
		StructuredContent json.RawMessage `json:"structuredContent"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("mcp: tool result: %w", err)
	}
	c.mu.Lock()
	modern := c.modern
	c.mu.Unlock()
	if modern {
		switch res.ResultType {
		case "complete":
		case "input_required":
			return nil, fmt.Errorf("mcp: 多轮请求（input_required）暂不支持")
		default:
			return nil, fmt.Errorf("mcp: unexpected resultType %q", res.ResultType)
		}
	}
	var text strings.Builder
	var attachments []map[string]any
	for _, item := range res.Content {
		switch item.Type {
		case "text":
			text.WriteString(item.Text)
		case "image", "audio":
			attachments = append(attachments, attachmentPlaceholder(item.Type, item.MimeType, item.Data))
		case "resource", "resource_link":
			attachments = append(attachments, map[string]any{
				"type": item.Type, "uri": item.URI, "mimeType": item.MimeType, "name": item.Name,
			})
		default:
			attachments = append(attachments, map[string]any{"type": item.Type})
		}
	}
	out := map[string]any{}
	body := text.String()
	if len(body) > maxTextResult {
		body = strings.ToValidUTF8(body[:maxTextResult], "") + "\n…（MCP 文本已截断）"
	}
	if body != "" {
		out["content"] = body
	}
	if len(attachments) > 0 {
		out["attachments"] = attachments
	}
	if len(res.StructuredContent) > 0 {
		if len(res.StructuredContent) > maxStructured {
			out["structuredContent"] = map[string]any{"truncated": true, "bytes": len(res.StructuredContent)}
		} else {
			var structured any
			if err := json.Unmarshal(res.StructuredContent, &structured); err == nil {
				out["structuredContent"] = structured
			}
		}
	}
	if res.IsError {
		msg := body
		if msg == "" {
			msg = "MCP 工具返回错误"
		}
		return nil, fmt.Errorf("%s", msg)
	}
	if len(out) == 0 {
		out["content"] = ""
	}
	return out, nil
}

func attachmentPlaceholder(kind, mimeType, base64Data string) map[string]any {
	placeholder := map[string]any{"type": kind, "mimeType": mimeType, "bytes": len(base64Data) * 3 / 4}
	if len(base64Data) <= 8<<20 {
		if data, err := decodeBase64(base64Data); err == nil {
			placeholder["bytes"] = len(data)
			placeholder["sha256"] = hashBytes(data)
		}
	}
	return placeholder
}
