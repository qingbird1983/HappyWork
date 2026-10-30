// Package deepseek implements the DeepSeek (OpenAI-compatible) provider with
// the standard library only (design.md §7): net/http for the request and a
// hand-written SSE parser. No SDK.
package deepseek

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/happywork/happywork/internal/model"
)

// Defaults from research/02 and design.md §7.1.
const (
	DefaultBaseURL = "https://api.deepseek.com"
	DefaultModel   = "deepseek-flash"

	dialTimeout      = 15 * time.Second
	firstByteTimeout = 30 * time.Second
	streamIdleLimit  = 120 * time.Second
)

// Sentinel errors mapped onto user-visible notices (design.md §7.3).
var (
	ErrUnauthorized        = errors.New("deepseek: unauthorized (check DEEPSEEK_API_KEY)")
	ErrRateLimited         = errors.New("deepseek: rate limited")
	ErrProviderUnavailable = errors.New("deepseek: provider unavailable")
	ErrMalformedStream     = errors.New("deepseek: malformed stream")
)

// Config is the provider configuration. APIKey comes only from the
// environment; callers must never persist or log it.
type Config struct {
	APIKey  string
	BaseURL string
	Model   string
	// Thinking enables reasoning output. Disabled by default: tool loops are
	// more deterministic and cheaper (design.md §2).
	Thinking   bool
	HTTPClient *http.Client
}

// Client implements model.Provider.
type Client struct {
	apiKey   string
	endpoint string
	model    string
	thinking bool
	http     *http.Client
}

// New validates the configuration and builds a client.
func New(cfg Config) (*Client, error) {
	key := strings.TrimSpace(cfg.APIKey)
	if key == "" {
		return nil, errors.New("deepseek: API key is empty")
	}
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = DefaultBaseURL
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("deepseek: invalid base URL %q", base)
	}
	name := strings.TrimSpace(cfg.Model)
	if name == "" {
		name = DefaultModel
	}
	hc := cfg.HTTPClient
	if hc == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.DialContext = (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext
		transport.ResponseHeaderTimeout = firstByteTimeout
		hc = &http.Client{Transport: transport}
	}
	return &Client{
		apiKey:   key,
		endpoint: strings.TrimRight(base, "/") + "/chat/completions",
		model:    name,
		thinking: cfg.Thinking,
		http:     hc,
	}, nil
}

// Model returns the configured model id.
func (c *Client) Model() string { return c.model }

// request body shapes (OpenAI compatible).
type chatRequest struct {
	Model         string          `json:"model"`
	Messages      []wireMessage   `json:"messages"`
	Tools         []wireTool      `json:"tools,omitempty"`
	Stream        bool            `json:"stream"`
	StreamOptions *streamOptions  `json:"stream_options,omitempty"`
	Thinking      *thinkingOption `json:"thinking,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type thinkingOption struct {
	Type string `json:"type"`
}

type wireMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type wireToolCall struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Function wireToolCallFunction `json:"function"`
}

// wireToolCallFunction carries arguments as a JSON string (OpenAI wire shape).
type wireToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// wireToolFunction describes a tool; parameters stay a JSON object.
type wireToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type wireTool struct {
	Type     string           `json:"type"`
	Function wireToolFunction `json:"function"`
}

func buildRequest(modelName string, thinking bool, msgs []model.Message, tools []model.ToolSpec) chatRequest {
	req := chatRequest{
		Model:         modelName,
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
		Thinking:      &thinkingOption{Type: "disabled"},
	}
	if thinking {
		req.Thinking.Type = "enabled"
	}
	req.Messages = make([]wireMessage, 0, len(msgs))
	for _, m := range msgs {
		wm := wireMessage{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			args := tc.Arguments
			if len(args) == 0 {
				args = json.RawMessage(`{}`)
			}
			wm.ToolCalls = append(wm.ToolCalls, wireToolCall{
				ID:       tc.ID,
				Type:     "function",
				Function: wireToolCallFunction{Name: tc.Name, Arguments: string(args)},
			})
		}
		req.Messages = append(req.Messages, wm)
	}
	req.Tools = make([]wireTool, 0, len(tools))
	for _, t := range tools {
		params := t.Parameters
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		req.Tools = append(req.Tools, wireTool{
			Type: "function",
			Function: wireToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			},
		})
	}
	return req
}

// Stream implements model.Provider.
func (c *Client) Stream(ctx context.Context, msgs []model.Message, tools []model.ToolSpec) <-chan model.Chunk {
	ch := make(chan model.Chunk, 32)
	go func() {
		defer close(ch)
		c.run(ctx, msgs, tools, ch)
	}()
	return ch
}
