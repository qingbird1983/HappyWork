package deepseek

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/happywork/happywork/internal/model"
)

const testKey = "sk-test-secret-value"

func collect(t *testing.T, ch <-chan model.Chunk) (string, []model.ToolCall, *model.Usage, error) {
	t.Helper()
	var text strings.Builder
	var calls []model.ToolCall
	var usage *model.Usage
	for chunk := range ch {
		switch {
		case chunk.Err != nil:
			return text.String(), calls, usage, chunk.Err
		case chunk.ToolCall != nil:
			calls = append(calls, *chunk.ToolCall)
		case chunk.Done:
			usage = chunk.Usage
			return text.String(), calls, usage, nil
		default:
			text.WriteString(chunk.Delta)
		}
	}
	return text.String(), calls, usage, errors.New("channel closed without Done")
}

func newTestClient(t *testing.T, h http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(Config{APIKey: testKey, BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func sse(h http.ResponseWriter, lines ...string) {
	h.Header().Set("Content-Type", "text/event-stream")
	fl := h.(http.Flusher)
	for _, line := range lines {
		_, _ = io.WriteString(h, line+"\n\n")
		fl.Flush()
	}
}

func TestStreamTextAndUsage(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+testKey {
			t.Errorf("authorization = %q", got)
		}
		sse(w,
			`data: {"choices":[{"delta":{"content":"你"}}]}`,
			`data: {"choices":[{"delta":{"content":"好"}}]}`,
			`data: {"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"prompt_cache_hit_tokens":4,"prompt_cache_miss_tokens":6,"completion_tokens_details":{"reasoning_tokens":1}}}`,
			`data: [DONE]`,
		)
	}))
	text, calls, usage, err := collect(t, c.Stream(context.Background(), []model.Message{{Role: "user", Content: "hi"}}, nil))
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if text != "你好" || len(calls) != 0 {
		t.Fatalf("text=%q calls=%v", text, calls)
	}
	if usage == nil || usage.TotalTokens != 12 || usage.CacheHitTokens != 4 || usage.CacheMissTokens != 6 || usage.ReasoningTokens != 1 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestStreamToolCallsFragmented(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sse(w,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_","arguments":""}}]}}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"file","arguments":"{\"pa"}}]}}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"a.md\"}"}}]}}]}`,
			`data: [DONE]`,
		)
	}))
	_, calls, _, err := collect(t, c.Stream(context.Background(), []model.Message{{Role: "user", Content: "read"}}, nil))
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if len(calls) != 1 || calls[0].ID != "call_1" || calls[0].Name != "read_file" {
		t.Fatalf("calls = %+v", calls)
	}
	var args map[string]string
	if err := json.Unmarshal(calls[0].Arguments, &args); err != nil || args["path"] != "a.md" {
		t.Fatalf("arguments = %s, err=%v", calls[0].Arguments, err)
	}
}

func TestStreamInvalidToolArguments(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sse(w,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"read_file","arguments":"{not json"}}]}}]}`,
			`data: [DONE]`,
		)
	}))
	_, _, _, err := collect(t, c.Stream(context.Background(), nil, nil))
	if !errors.Is(err, ErrMalformedStream) {
		t.Fatalf("err = %v, want ErrMalformedStream", err)
	}
}

func TestStreamTruncated(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sse(w, `data: {"choices":[{"delta":{"content":"半"}}]}`)
	}))
	_, _, _, err := collect(t, c.Stream(context.Background(), nil, nil))
	if !errors.Is(err, ErrMalformedStream) {
		t.Fatalf("err = %v, want ErrMalformedStream", err)
	}
}

func TestStreamMalformedEvent(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sse(w, `data: {broken`, `data: [DONE]`)
	}))
	_, _, _, err := collect(t, c.Stream(context.Background(), nil, nil))
	if !errors.Is(err, ErrMalformedStream) {
		t.Fatalf("err = %v, want ErrMalformedStream", err)
	}
}

func TestStatusErrors(t *testing.T) {
	cases := []struct {
		status int
		retry  string
		want   error
	}{
		{http.StatusUnauthorized, "", ErrUnauthorized},
		{http.StatusForbidden, "", ErrUnauthorized},
		{http.StatusTooManyRequests, "3", ErrRateLimited},
		{http.StatusInternalServerError, "", ErrProviderUnavailable},
		{http.StatusBadGateway, "", ErrProviderUnavailable},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.retry != "" {
					w.Header().Set("Retry-After", tc.retry)
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, `{"error":"nope"}`)
			}))
			_, _, _, err := collect(t, c.Stream(context.Background(), nil, nil))
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if strings.Contains(err.Error(), testKey) {
				t.Fatalf("error leaked the API key: %v", err)
			}
		})
	}
}

func TestRequestShape(t *testing.T) {
	var captured map[string]any
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Errorf("request json: %v", err)
		}
		sse(w, `data: [DONE]`)
	}))
	msgs := []model.Message{
		{Role: "system", Content: "sys"},
		{Role: "assistant", Content: "calling", ToolCalls: []model.ToolCall{{ID: "c1", Name: "read_file", Arguments: json.RawMessage(`{"path":"a"}`)}}},
		{Role: "tool", Content: `{"ok":true}`, ToolCallID: "c1"},
	}
	tools := []model.ToolSpec{{Name: "read_file", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)}}
	if _, _, _, err := collect(t, c.Stream(context.Background(), msgs, tools)); err != nil {
		t.Fatal(err)
	}
	if captured["model"] != DefaultModel || captured["stream"] != true {
		t.Fatalf("request head = %+v", captured)
	}
	opts := captured["stream_options"].(map[string]any)
	if opts["include_usage"] != true {
		t.Fatalf("stream_options = %+v", opts)
	}
	thinking := captured["thinking"].(map[string]any)
	if thinking["type"] != "disabled" {
		t.Fatalf("thinking = %+v", thinking)
	}
	wireMsgs := captured["messages"].([]any)
	if len(wireMsgs) != 3 {
		t.Fatalf("messages = %+v", wireMsgs)
	}
	assistant := wireMsgs[1].(map[string]any)
	call := assistant["tool_calls"].([]any)[0].(map[string]any)
	fn := call["function"].(map[string]any)
	if call["id"] != "c1" || call["type"] != "function" || fn["name"] != "read_file" || fn["arguments"] != `{"path":"a"}` {
		t.Fatalf("assistant tool call = %+v", call)
	}
	toolMsg := wireMsgs[2].(map[string]any)
	if toolMsg["tool_call_id"] != "c1" {
		t.Fatalf("tool message = %+v", toolMsg)
	}
	wireTools := captured["tools"].([]any)
	tool := wireTools[0].(map[string]any)
	if tool["type"] != "function" || tool["function"].(map[string]any)["name"] != "read_file" {
		t.Fatalf("tools = %+v", wireTools)
	}
	if _, ok := tool["function"].(map[string]any)["parameters"].(map[string]any); !ok {
		t.Fatalf("parameters not an object: %+v", tool)
	}
}

func TestThinkingEnabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["thinking"].(map[string]any)["type"] != "enabled" {
			t.Errorf("thinking = %+v", body["thinking"])
		}
		sse(w, `data: [DONE]`)
	}))
	defer srv.Close()
	c, err := New(Config{APIKey: testKey, BaseURL: srv.URL, Thinking: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := collect(t, c.Stream(context.Background(), nil, nil)); err != nil {
		t.Fatal(err)
	}
}

func TestConfigValidation(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("empty key must fail")
	}
	if _, err := New(Config{APIKey: "k", BaseURL: "ftp://nope"}); err == nil {
		t.Fatal("bad scheme must fail")
	}
	c, err := New(Config{APIKey: "k"})
	if err != nil || c.Model() != DefaultModel {
		t.Fatalf("defaults: %+v, %v", c, err)
	}
}
