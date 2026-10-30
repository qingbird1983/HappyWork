package mcp

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/happywork/happywork/internal/tool"
)

// TestMCPHelper is the child-process MCP server used by the tests. It is a
// no-op unless MCP_HELPER_MODE is set (the parent passes it through config env).
func TestMCPHelper(t *testing.T) {
	mode := os.Getenv("MCP_HELPER_MODE")
	if mode == "" {
		t.Skip("helper process only")
	}
	serveHelper(mode, os.Getenv("MCP_HELPER_PAGINATION") == "1")
	os.Exit(0) // avoid test-framework output on the protocol channel
}

type helperRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func serveHelper(mode string, pagination bool) {
	enc := json.NewEncoder(os.Stdout)
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var req helperRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}
		if len(req.ID) == 0 {
			continue // notification
		}
		respond := func(result any) {
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "result": result})
		}
		fail := func(code int, msg string) {
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "error": map[string]any{"code": code, "message": msg}})
		}
		switch req.Method {
		case "server/discover":
			if mode == "modern" {
				var p map[string]any
				_ = json.Unmarshal(req.Params, &p)
				if _, ok := p["_meta"]; !ok {
					fail(-32602, "missing _meta")
					continue
				}
				respond(map[string]any{"resultType": "complete"})
			} else {
				fail(-32601, "method not found")
			}
		case "initialize":
			if mode == "legacy" {
				respond(map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}})
			} else {
				fail(-32601, "method not found")
			}
		case "tools/list":
			var p map[string]any
			_ = json.Unmarshal(req.Params, &p)
			if mode == "modern" && pagination {
				if p["cursor"] == nil {
					respond(map[string]any{"resultType": "complete", "tools": helperTools("echo"), "nextCursor": "page2"})
				} else {
					respond(map[string]any{"resultType": "complete", "tools": helperTools("image")})
				}
				continue
			}
			tools := helperTools("echo", "image", "fail", "slow", "input", "env", "badref")
			result := map[string]any{"tools": tools}
			if mode == "modern" {
				result["resultType"] = "complete"
			}
			respond(result)
		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			result := map[string]any{}
			switch p.Name {
			case "echo":
				result["content"] = []map[string]any{{"type": "text", "text": "echo: " + fmt.Sprint(p.Arguments["text"])}}
			case "image":
				result["content"] = []map[string]any{{
					"type": "image", "mimeType": "image/png",
					"data": base64.StdEncoding.EncodeToString([]byte("abc")),
				}}
			case "fail":
				result["isError"] = true
				result["content"] = []map[string]any{{"type": "text", "text": "boom"}}
			case "slow":
				time.Sleep(3 * time.Second)
				result["content"] = []map[string]any{{"type": "text", "text": "late"}}
			case "input":
				if mode == "modern" {
					result["resultType"] = "input_required"
				}
			case "env":
				_, hasKey := os.LookupEnv("DEEPSEEK_API_KEY")
				result["content"] = []map[string]any{{
					"type": "text",
					"text": fmt.Sprintf("mode=%s key=%v helper=%s", mode, hasKey, os.Getenv("MCP_HELPER_MODE")),
				}}
			case "legacyecho":
				result["content"] = []map[string]any{{"type": "text", "text": "legacy echo"}}
			default:
				fail(-32602, "unknown tool")
				continue
			}
			if mode == "modern" {
				if _, ok := result["resultType"]; !ok {
					result["resultType"] = "complete"
				}
			}
			respond(result)
		default:
			fail(-32601, "method not found")
		}
	}
}

func helperTools(names ...string) []map[string]any {
	out := make([]map[string]any, 0, len(names))
	for _, name := range names {
		schema := map[string]any{
			"type":       "object",
			"properties": map[string]any{"text": map[string]any{"type": "string"}},
		}
		if name == "badref" {
			schema["properties"] = map[string]any{"x": map[string]any{"$ref": "#/defs/x"}}
		}
		out = append(out, map[string]any{"name": name, "description": "helper " + name, "inputSchema": schema})
	}
	return out
}

func newHelperClient(t *testing.T, mode string, pagination bool) *Client {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MCP_HELPER_MODE", mode)
	if pagination {
		t.Setenv("MCP_HELPER_PAGINATION", "1")
	}
	env := map[string]string{"MCP_HELPER_MODE": "$ENV:MCP_HELPER_MODE"}
	if pagination {
		env["MCP_HELPER_PAGINATION"] = "$ENV:MCP_HELPER_PAGINATION"
	}
	cfg := ServerConfig{
		Command: exe,
		Args:    []string{"-test.run=^TestMCPHelper$"},
		Env:     env,
	}
	c := NewClient("demo", cfg, t.TempDir(), log.New(os.Stderr, "mcptest ", 0))
	t.Cleanup(c.Close)
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return c
}

func findTool(t *testing.T, c *Client, provider string) toolRunner {
	t.Helper()
	for _, tl := range c.Tools() {
		if tl.Meta().Name == provider {
			return tl
		}
	}
	t.Fatalf("tool %s not discovered; have %v", provider, toolNames(c))
	return nil
}

type toolRunner interface {
	Meta() tool.Meta
	Exec(ctx context.Context, args json.RawMessage) (any, error)
}

func toolNames(c *Client) []string {
	var names []string
	for _, tl := range c.Tools() {
		names = append(names, tl.Meta().Name)
	}
	return names
}

func TestModernProbeDiscoveryAndCall(t *testing.T) {
	c := newHelperClient(t, "modern", false)
	names := toolNames(c)
	if len(names) != 6 {
		t.Fatalf("tools = %v, want 6 (badref skipped)", names)
	}
	echo := findTool(t, c, "mcp__demo__echo")
	out, err := echo.Exec(context.Background(), json.RawMessage(`{"text":"hi"}`))
	if err != nil {
		t.Fatalf("echo: %v", err)
	}
	payload := out.(map[string]any)
	if !strings.Contains(fmt.Sprint(payload["content"]), "echo: hi") {
		t.Fatalf("payload = %+v", payload)
	}
	if echo.Meta().Trust != "untrusted" || echo.Meta().Source != "mcp:demo" || echo.Meta().Ref != "mcp://demo/echo" {
		t.Fatalf("meta = %+v", echo.Meta())
	}
}

func TestLegacyProbeAndCall(t *testing.T) {
	c := newHelperClient(t, "legacy", false)
	echo := findTool(t, c, "mcp__demo__echo")
	out, err := echo.Exec(context.Background(), json.RawMessage(`{"text":"hi"}`))
	if err != nil {
		t.Fatalf("echo: %v", err)
	}
	if !strings.Contains(fmt.Sprint(out.(map[string]any)["content"]), "echo: hi") {
		t.Fatalf("payload = %+v", out)
	}
}

func TestPagination(t *testing.T) {
	c := newHelperClient(t, "modern", true)
	names := toolNames(c)
	if len(names) != 2 {
		t.Fatalf("paged tools = %v", names)
	}
}

func TestImagePlaceholderAndErrors(t *testing.T) {
	c := newHelperClient(t, "modern", false)
	image := findTool(t, c, "mcp__demo__image")
	out, err := image.Exec(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("image: %v", err)
	}
	attachments := out.(map[string]any)["attachments"].([]map[string]any)
	if len(attachments) != 1 || attachments[0]["type"] != "image" || attachments[0]["mimeType"] != "image/png" {
		t.Fatalf("attachments = %+v", attachments)
	}
	if attachments[0]["bytes"] != 3 || attachments[0]["sha256"] == "" {
		t.Fatalf("image placeholder = %+v", attachments[0])
	}

	fail := findTool(t, c, "mcp__demo__fail")
	if _, err := fail.Exec(context.Background(), json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("fail err = %v", err)
	}
	input := findTool(t, c, "mcp__demo__input")
	if _, err := input.Exec(context.Background(), json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "input_required") {
		t.Fatalf("input err = %v", err)
	}
}

func TestEnvironmentIsolation(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "must-not-leak")
	c := newHelperClient(t, "modern", false)
	env := findTool(t, c, "mcp__demo__env")
	out, err := env.Exec(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	text := fmt.Sprint(out.(map[string]any)["content"])
	if !strings.Contains(text, "key=false") || !strings.Contains(text, "helper=modern") {
		t.Fatalf("env result = %s", text)
	}
}

func TestCallTimeoutKillsAndRestarts(t *testing.T) {
	c := newHelperClient(t, "modern", false)
	c.cfg.TimeoutSeconds = 1
	slow := findTool(t, c, "mcp__demo__slow")
	start := time.Now()
	if _, err := slow.Exec(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("slow call should time out")
	}
	if time.Since(start) > 6*time.Second {
		t.Fatalf("timeout took %v", time.Since(start))
	}
	// The client restarts for the next call.
	echo := findTool(t, c, "mcp__demo__echo")
	out, err := echo.Exec(context.Background(), json.RawMessage(`{"text":"again"}`))
	if err != nil {
		t.Fatalf("echo after restart: %v", err)
	}
	if !strings.Contains(fmt.Sprint(out.(map[string]any)["content"]), "again") {
		t.Fatalf("payload after restart = %+v", out)
	}
}

func TestConfigLoadValidation(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) string {
		path := filepath.Join(dir, "mcp.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	if cfg, err := Load(filepath.Join(dir, "missing.json")); err != nil || len(cfg.Servers) != 0 {
		t.Fatalf("missing file: %+v %v", cfg, err)
	}
	valid := `{"servers":{"demo":{"command":"` + filepath.ToSlash(os.Args[0]) + `","enabled":true}}}`
	if filepath.IsAbs(os.Args[0]) {
		if _, err := Load(write(valid)); err != nil {
			t.Fatalf("valid config: %v", err)
		}
	}
	for name, body := range map[string]string{
		"relative":  `{"servers":{"demo":{"command":"demo-server"}}}`,
		"unknown":   `{"servers":{"demo":{"command":"C:/x","extra":1}}}`,
		"literal":   `{"servers":{"demo":{"command":"C:/x","env":{"TOKEN":"plain"}}}}`,
		"badaction": `{"servers":{"demo":{"command":"C:/x","tools":{"t":{"action":"run"}}}}}`,
	} {
		if _, err := Load(write(body)); err == nil {
			t.Errorf("%s config should fail", name)
		}
	}
}

func TestManagerStartAndSnapshot(t *testing.T) {
	exe, _ := os.Executable()
	t.Setenv("MCP_HELPER_MODE", "modern")
	cfg := Config{Servers: map[string]ServerConfig{
		"demo": {Command: exe, Args: []string{"-test.run=^TestMCPHelper$"}, Env: map[string]string{"MCP_HELPER_MODE": "$ENV:MCP_HELPER_MODE"}},
		"off":  {Command: exe, Enabled: boolPtr(false)},
	}}
	m := NewManager(cfg, t.TempDir(), log.New(os.Stderr, "mcpmanager ", 0))
	m.Start(context.Background())
	defer m.Close()
	if n := len(m.Tools()); n != 6 {
		t.Fatalf("manager tools = %d, want 6", n)
	}
	status := m.Status()
	if len(status) != 1 || status[0].Name != "demo" || !status[0].Modern || status[0].Disabled {
		t.Fatalf("status = %+v", status)
	}
}

func boolPtr(v bool) *bool { return &v }
