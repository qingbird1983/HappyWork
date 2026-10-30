package agent

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/happywork/happywork/internal/model"
	"github.com/happywork/happywork/internal/storage"
)

// Resume continues a paused run (approval timeout or budget) or an interrupted
// session (crash recovery). Paused runs are woken in place; interrupted runs
// replay the current turn with explicit "outcome unknown" tool results.
func (s *Session) Resume(ctx context.Context) error {
	s.mu.Lock()
	state := s.state
	running := s.running
	s.mu.Unlock()
	switch state {
	case StatePaused:
		if !running {
			return ErrNotResumable
		}
		select {
		case s.resumeCh <- struct{}{}:
		default:
		}
		return nil
	case StateInterrupted:
		return s.enqueueStart(cmdResumeInterrupted, ctx, "")
	default:
		return ErrNotResumable
	}
}

// Discard abandons an interrupted session (transcript is kept).
func (s *Session) Discard(ctx context.Context) error {
	s.mu.Lock()
	if s.state != StateInterrupted {
		s.mu.Unlock()
		return ErrNotResumable
	}
	if s.running || s.starting {
		s.mu.Unlock()
		return ErrBusy
	}
	s.starting = true
	s.mu.Unlock()

	reply := make(chan error, 1)
	select {
	case s.cmds <- command{kind: cmdDiscard, ctx: ctx, reply: reply}:
	case <-ctx.Done():
		s.mu.Lock()
		s.starting = false
		s.mu.Unlock()
		return ctx.Err()
	}
	return <-reply
}

// injectUnknownResults fills missing tool results after a crash. The model must
// be told the side effects are unknown, not that they failed.
func (s *Session) injectUnknownResults(ctx context.Context) {
	msgs, err := s.mgr.store.Transcript(ctx, s.info.ID)
	if err != nil {
		s.mgr.log.Printf("agent: recovery transcript: %v", err)
		return
	}
	answered := map[string]bool{}
	for _, m := range msgs {
		if m.Role == storage.RoleTool && m.ToolCallID != "" {
			answered[m.ToolCallID] = true
		}
	}
	for _, m := range msgs {
		if m.Role != storage.RoleAssistant || m.ToolCallsJSON == "" {
			continue
		}
		var calls []model.ToolCall
		if err := json.Unmarshal([]byte(m.ToolCallsJSON), &calls); err != nil {
			continue
		}
		for _, call := range calls {
			if answered[call.ID] {
				continue
			}
			result := storage.Message{
				ID: storage.NewID(), SessionID: s.info.ID,
				Role: storage.RoleTool, ToolCallID: call.ID, ToolName: call.Name, TurnID: m.TurnID,
				Content: storage.MustJSON(map[string]string{
					"error": "应用中断，执行结果未知；副作用是否发生不确定",
				}),
			}
			if strings.HasPrefix(call.Name, "mcp__") {
				result.Trust = storage.TrustUntrusted
				s.markUntrusted()
			}
			if err := s.mgr.store.AppendMessage(ctx, &result); err != nil {
				s.mgr.log.Printf("agent: append recovery result: %v", err)
			}
		}
	}
}
