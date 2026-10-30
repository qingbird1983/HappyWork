package deepseek

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/happywork/happywork/internal/model"
)

// newSSEScanner reads newline-delimited SSE data with a 1 MiB line cap.
func newSSEScanner(r io.Reader) *bufio.Scanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	return sc
}

type sseResponse struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens          int `json:"prompt_tokens"`
		CompletionTokens      int `json:"completion_tokens"`
		TotalTokens           int `json:"total_tokens"`
		PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens"`
		PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"`
		ReasoningTokens       int `json:"reasoning_tokens"`
		CompletionDetails     struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
	} `json:"usage"`
}

type callAccumulator struct {
	id    string
	name  strings.Builder
	args  strings.Builder
	index int
}

func (c *Client) run(ctx context.Context, msgs []model.Message, tools []model.ToolSpec, ch chan<- model.Chunk) {
	reqBody := buildRequest(c.model, c.thinking, msgs, tools)
	payload, err := json.Marshal(reqBody)
	if err != nil {
		sendErr(ctx, ch, fmt.Errorf("%w: encode request: %v", ErrMalformedStream, err))
		return
	}
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(streamCtx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		sendErr(ctx, ch, fmt.Errorf("%w: build request: %v", ErrProviderUnavailable, err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	var idle atomic.Bool
	activity := make(chan struct{}, 1)
	go watchIdle(streamCtx, cancel, activity, &idle)

	resp, err := c.http.Do(req)
	if err != nil {
		sendErr(ctx, ch, classifyTransport(streamCtx, ctx, &idle, err))
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		sendErr(ctx, ch, statusError(resp))
		return
	}

	// SSE framing: one data line per event, capped at 1 MiB (design.md §7.2).
	reader := newSSEScanner(resp.Body)
	accs := map[int]*callAccumulator{}
	var usage *model.Usage
	sawDone := false
	for reader.Scan() {
		signal(activity)
		line := strings.TrimSpace(reader.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			sawDone = true
			break
		}
		var event sseResponse
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			sendErr(ctx, ch, fmt.Errorf("%w: parse event: %v", ErrMalformedStream, err))
			return
		}
		for _, choice := range event.Choices {
			if choice.Delta.Content != "" {
				if !sendChunk(streamCtx, ch, model.Chunk{Delta: choice.Delta.Content}) {
					return
				}
			}
			for _, tc := range choice.Delta.ToolCalls {
				acc := accs[tc.Index]
				if acc == nil {
					acc = &callAccumulator{index: tc.Index}
					accs[tc.Index] = acc
				}
				if acc.id == "" && tc.ID != "" {
					acc.id = tc.ID
				}
				acc.name.WriteString(tc.Function.Name)
				acc.args.WriteString(tc.Function.Arguments)
			}
		}
		if event.Usage != nil {
			usage = &model.Usage{
				PromptTokens:     event.Usage.PromptTokens,
				CompletionTokens: event.Usage.CompletionTokens,
				TotalTokens:      event.Usage.TotalTokens,
				CacheHitTokens:   event.Usage.PromptCacheHitTokens,
				CacheMissTokens:  event.Usage.PromptCacheMissTokens,
				ReasoningTokens:  event.Usage.ReasoningTokens,
			}
			if usage.ReasoningTokens == 0 {
				usage.ReasoningTokens = event.Usage.CompletionDetails.ReasoningTokens
			}
		}
	}
	if err := reader.Err(); err != nil {
		sendErr(ctx, ch, classifyReadError(ctx, &idle, err))
		return
	}
	if !sawDone {
		if streamCtx.Err() != nil {
			sendErr(ctx, ch, classifyReadError(ctx, &idle, streamCtx.Err()))
			return
		}
		sendErr(ctx, ch, fmt.Errorf("%w: stream ended before [DONE]", ErrMalformedStream))
		return
	}

	// Emit complete tool calls in index order only after validating arguments:
	// the actor must never execute a partially assembled call.
	indexes := make([]int, 0, len(accs))
	for i := range accs {
		indexes = append(indexes, i)
	}
	sort.Ints(indexes)
	for _, i := range indexes {
		acc := accs[i]
		name := acc.name.String()
		args := strings.TrimSpace(acc.args.String())
		if acc.id == "" || name == "" {
			sendErr(ctx, ch, fmt.Errorf("%w: tool call missing id/name", ErrMalformedStream))
			return
		}
		if args == "" {
			args = "{}"
		}
		if !json.Valid([]byte(args)) {
			sendErr(ctx, ch, fmt.Errorf("%w: tool call %q arguments are not valid JSON", ErrMalformedStream, name))
			return
		}
		call := model.ToolCall{ID: acc.id, Name: name, Arguments: json.RawMessage(args)}
		if !sendChunk(streamCtx, ch, model.Chunk{ToolCall: &call}) {
			return
		}
	}
	_ = sendChunk(streamCtx, ch, model.Chunk{Done: true, Usage: usage})
}

// watchIdle cancels the request when the stream produces no line for the idle
// limit. Activity signals reset the timer.
func watchIdle(ctx context.Context, cancel context.CancelFunc, activity <-chan struct{}, idle *atomic.Bool) {
	timer := time.NewTimer(streamIdleLimit)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-activity:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(streamIdleLimit)
		case <-timer.C:
			idle.Store(true)
			cancel()
			return
		}
	}
}

func signal(ch chan<- struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func sendChunk(ctx context.Context, ch chan<- model.Chunk, c model.Chunk) bool {
	select {
	case ch <- c:
		return true
	case <-ctx.Done():
		return false
	}
}

func sendErr(ctx context.Context, ch chan<- model.Chunk, err error) {
	select {
	case ch <- model.Chunk{Err: err}:
	case <-ctx.Done():
	}
}

func classifyTransport(streamCtx, outer context.Context, idle *atomic.Bool, err error) error {
	if idle.Load() {
		return fmt.Errorf("%w: stream idle timeout", ErrProviderUnavailable)
	}
	if outer.Err() != nil {
		return outer.Err()
	}
	if streamCtx.Err() != nil {
		return fmt.Errorf("%w: %v", ErrProviderUnavailable, streamCtx.Err())
	}
	return fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
}

func classifyReadError(outer context.Context, idle *atomic.Bool, err error) error {
	if idle.Load() {
		return fmt.Errorf("%w: stream idle timeout", ErrProviderUnavailable)
	}
	if outer.Err() != nil {
		return outer.Err()
	}
	return fmt.Errorf("%w: %v", ErrMalformedStream, err)
}

func statusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	snippet := strings.TrimSpace(string(body))
	if len(snippet) > 400 {
		snippet = snippet[:400] + "…"
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w (HTTP %d)", ErrUnauthorized, resp.StatusCode)
	case http.StatusTooManyRequests:
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			return fmt.Errorf("%w (retry after %s)", ErrRateLimited, ra)
		}
		return ErrRateLimited
	default:
		if snippet != "" {
			return fmt.Errorf("%w (HTTP %d: %s)", ErrProviderUnavailable, resp.StatusCode, snippet)
		}
		return fmt.Errorf("%w (HTTP %d)", ErrProviderUnavailable, resp.StatusCode)
	}
}
