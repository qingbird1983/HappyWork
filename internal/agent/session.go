package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/happywork/happywork/internal/model"
	"github.com/happywork/happywork/internal/storage"
	"github.com/happywork/happywork/internal/tool"
	"github.com/happywork/happywork/internal/workspace"
)

// Session drives one conversation: the deterministic turn loop. Send is
// serialized per session; Cancel is safe from any goroutine.
type Session struct {
	mgr   *Manager
	info  storage.Session
	tools *tool.Registry

	mu      sync.Mutex
	running atomic.Bool
	cancel  context.CancelFunc
	state   string
}

// ID returns the session identifier.
func (s *Session) ID() string { return s.info.ID }

// State returns the current state.
func (s *Session) State() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == "" {
		return s.info.State
	}
	return s.state
}

// Info returns a copy of the session metadata.
func (s *Session) Info() storage.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.info
}

// Send submits one user message and runs turns until the model stops calling
// tools. It returns immediately; the run continues in its own goroutine and
// reports progress through events. Re-entrant sends fail with ErrBusy.
func (s *Session) Send(ctx context.Context, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("agent: empty message")
	}
	if !s.running.CompareAndSwap(false, true) {
		return ErrBusy
	}

	runCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()

	go func() {
		defer func() {
			cancel()
			s.running.Store(false)
		}()
		s.run(runCtx, text)
	}()
	return nil
}

// Cancel aborts the in-flight run; the loop exits at its next checkpoint and
// the session transitions to cancelled. No-op when idle.
func (s *Session) Cancel() {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// setState persists and broadcasts a state transition.
func (s *Session) setState(state string) {
	s.mu.Lock()
	s.state = state
	s.info.State = state
	s.mu.Unlock()
	if err := s.mgr.store.UpdateSessionState(context.Background(), s.info.ID, state); err != nil {
		s.mgr.log.Printf("agent: persist state %s: %v", state, err)
	}
	s.mgr.emitEvent(s.info.ID, EventState, StatePayload{SessionID: s.info.ID, State: state})
}

// history assembles the provider conversation: system prompt plus persisted
// messages (assistant tool_calls restored from tool_calls_json).
func (s *Session) history(ctx context.Context) ([]model.Message, error) {
	msgs, err := s.mgr.store.Transcript(ctx, s.info.ID)
	if err != nil {
		return nil, err
	}
	out := make([]model.Message, 0, len(msgs)+1)
	out = append(out, model.Message{Role: model.RoleSystem, Content: systemPrompt})
	for _, m := range msgs {
		mm := model.Message{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		if m.ToolCallsJSON != "" {
			_ = json.Unmarshal([]byte(m.ToolCallsJSON), &mm.ToolCalls)
		}
		out = append(out, mm)
	}
	return out, nil
}

// run executes the turn loop for one user message.
func (s *Session) run(ctx context.Context, text string) {
	bg := context.Background()
	sessID := s.info.ID

	// Persist the user message; the first one also names the session.
	userMsg := storage.Message{ID: storage.NewID(), SessionID: sessID, Role: storage.RoleUser, Content: text}
	if err := s.mgr.store.AppendMessage(bg, &userMsg); err != nil {
		s.mgr.log.Printf("agent: persist user message: %v", err)
	}
	info := s.Info()
	if info.Title == "新会话" {
		_ = s.mgr.store.UpdateSessionTitle(bg, sessID, truncateTitle(text))
	}

	s.setState(StateThinking)

	for turn := 1; turn <= MaxTurns; turn++ {
		hist, err := s.history(bg)
		if err != nil {
			s.finish(StateError, fmt.Sprintf("load history: %v", err))
			return
		}

		assistText, calls, err := s.streamTurn(ctx, hist)
		if err != nil {
			// Keep whatever partial text was already streamed (design.md §6).
			if assistText != "" {
				partial := storage.Message{
					ID: storage.NewID(), SessionID: sessID,
					Role: storage.RoleAssistant, Content: assistText,
				}
				if appendErr := s.mgr.store.AppendMessage(bg, &partial); appendErr != nil {
					s.mgr.log.Printf("agent: persist partial message: %v", appendErr)
				}
			}
			s.finish(s.stateForErr(ctx), err.Error())
			return
		}

		if assistText != "" {
			am := storage.Message{
				ID: storage.NewID(), SessionID: sessID,
				Role: storage.RoleAssistant, Content: assistText,
			}
			if len(calls) > 0 {
				am.ToolCallsJSON = storage.MustJSON(calls)
			}
			if err := s.mgr.store.AppendMessage(bg, &am); err != nil {
				s.mgr.log.Printf("agent: persist assistant message: %v", err)
			}
		}

		if len(calls) == 0 {
			s.finish(StateCompleted, "")
			return
		}
		if ctx.Err() != nil {
			s.finish(StateCancelled, "")
			return
		}
		if turn == MaxTurns {
			s.finish(StateError, "max turns reached")
			return
		}

		results := s.execCalls(ctx, calls)
		if ctx.Err() != nil {
			s.finish(StateCancelled, "")
			return
		}
		for _, r := range results {
			tm := storage.Message{
				ID: storage.NewID(), SessionID: sessID,
				Role: storage.RoleTool, Content: r.content,
				ToolCallID: r.callID, ToolName: r.name,
			}
			if err := s.mgr.store.AppendMessage(bg, &tm); err != nil {
				s.mgr.log.Printf("agent: persist tool message: %v", err)
			}
		}
		s.setState(StateThinking)
	}
}

// streamTurn streams one provider turn. It returns the accumulated text and
// any tool calls requested by the model.
func (s *Session) streamTurn(ctx context.Context, hist []model.Message) (string, []model.ToolCall, error) {
	sessID := s.info.ID
	ch := s.mgr.prov.Stream(ctx, hist, s.tools.Specs())

	var (
		text    strings.Builder
		calls   []model.ToolCall
		batcher = newDeltaBatcher(func(joined string) {
			s.mgr.emitEvent(sessID, EventDelta, DeltaPayload{SessionID: sessID, Text: joined})
		})
	)

	for chunk := range ch {
		if err := ctx.Err(); err != nil {
			batcher.flush()
			return text.String(), calls, err
		}
		switch {
		case chunk.Err != nil:
			batcher.flush()
			return text.String(), calls, chunk.Err
		case chunk.Done:
			batcher.flush()
			return text.String(), calls, nil
		case chunk.ToolCall != nil:
			batcher.flush() // tool boundary: emit what we have
			calls = append(calls, *chunk.ToolCall)
			s.setState(StateRunningTool)
		default:
			text.WriteString(chunk.Delta)
			batcher.add(chunk.Delta)
		}
	}
	// The provider closed the channel; if that was caused by cancellation
	// (rather than a Done chunk), surface it so the run lands on cancelled.
	if err := ctx.Err(); err != nil {
		batcher.flush()
		return text.String(), calls, err
	}
	batcher.flush()
	return text.String(), calls, nil
}

// toolResult carries one executed call back into the transcript.
type toolResult struct {
	callID  string
	name    string
	content string
}

// execCalls runs each requested tool call, persisting tool_calls rows and
// audit entries for boundary denials (A5).
func (s *Session) execCalls(ctx context.Context, calls []model.ToolCall) []toolResult {
	sessID := s.info.ID
	out := make([]toolResult, 0, len(calls))

	for _, call := range calls {
		if ctx.Err() != nil {
			return out
		}
		s.mgr.emitEvent(sessID, EventTool, ToolPayload{SessionID: sessID, Name: call.Name, Status: "running"})

		record := storage.ToolCallRecord{
			ID: storage.NewID(), SessionID: sessID,
			Name: call.Name, ArgsJSON: string(call.Arguments),
			Status: "running",
		}
		if err := s.mgr.store.AppendToolCall(ctx, &record); err != nil {
			s.mgr.log.Printf("agent: persist tool call: %v", err)
		}

		resultJSON, status := s.execOne(ctx, call, record.ID)

		if err := s.mgr.store.UpdateToolCallResult(ctx, record.ID, resultJSON, status); err != nil {
			s.mgr.log.Printf("agent: persist tool result: %v", err)
		}
		s.mgr.emitEvent(sessID, EventTool, ToolPayload{SessionID: sessID, Name: call.Name, Status: status})
		out = append(out, toolResult{callID: call.ID, name: call.Name, content: resultJSON})
	}
	return out
}

// execOne runs a single call and returns (resultJSON, status).
func (s *Session) execOne(ctx context.Context, call model.ToolCall, recordID string) (string, string) {
	t, ok := s.tools.Get(call.Name)
	if !ok {
		return storage.MustJSON(map[string]string{"error": fmt.Sprintf("unknown tool %q", call.Name)}), "failed"
	}

	result, err := t.Exec(ctx, call.Arguments)
	if err != nil {
		status := "failed"
		if errors.Is(err, workspace.ErrOutsideWorkspace) {
			// A5: boundary denials leave an audit trail before the model sees them.
			if aerr := s.mgr.store.AppendAudit(ctx, s.info.ID, call.Name, "deny", err.Error()); aerr != nil {
				s.mgr.log.Printf("agent: persist audit: %v", aerr)
			}
		}
		return storage.MustJSON(map[string]string{"error": err.Error()}), status
	}
	return storage.MustJSON(result), "ok"
}

// stateForErr maps a loop error to the terminal state (cancelled keeps its
// own state; everything else is error).
func (s *Session) stateForErr(ctx context.Context) string {
	if ctx.Err() != nil {
		return StateCancelled
	}
	return StateError
}

// finish lands on a terminal state and emits done.
func (s *Session) finish(state, detail string) {
	s.setState(state)
	if detail != "" {
		s.mgr.log.Printf("agent: session %s finished %s: %s", s.info.ID, state, detail)
	}
	s.mgr.emitEvent(s.info.ID, EventDone, DonePayload{SessionID: s.info.ID, FinalState: state})
}

// truncateTitle derives the session title from the first user message.
func truncateTitle(text string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) > 40 {
		return string(runes[:40]) + "…"
	}
	return string(runes)
}
