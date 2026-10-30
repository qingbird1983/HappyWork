package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/happywork/happywork/internal/model"
	"github.com/happywork/happywork/internal/storage"
)

// waitResumed waits until a resumed run has left "interrupted". The run may
// pass through thinking and finish before the next poll, so do not require
// observing the intermediate state.
func waitResumed(t *testing.T, s *Session) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s.State() != StateInterrupted {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("session did not leave interrupted state")
}

func TestRecoveryResumeInjectsUnknown(t *testing.T) {
	f := newFixture(t)
	s := f.newSession(t)
	ctx := context.Background()
	user := storage.Message{ID: storage.NewID(), SessionID: s.ID(), Role: storage.RoleUser, Content: "写文件", TurnID: 1}
	if err := f.store.AppendMessage(ctx, &user); err != nil {
		t.Fatal(err)
	}
	call := toolCall("w1", "write_file", map[string]any{"path": "a.txt", "content": "x"})
	assistant := storage.Message{
		ID: storage.NewID(), SessionID: s.ID(), Role: storage.RoleAssistant,
		ToolCallsJSON: storage.MustJSON([]model.ToolCall{call}), TurnID: 1,
	}
	if err := f.store.AppendMessage(ctx, &assistant); err != nil {
		t.Fatal(err)
	}
	s.setState(StateInterrupted)

	if err := s.Send(ctx, "新消息"); !errors.Is(err, ErrNotResumable) {
		t.Fatalf("Send while interrupted err = %v", err)
	}
	f.prov.setScript([]model.Chunk{{Delta: "继续完成。"}, {Done: true}})
	if err := s.Resume(ctx); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	waitResumed(t, s)
	waitTerminal(t, f, s)
	if s.State() != StateCompleted {
		t.Fatalf("state = %s", s.State())
	}
	msgs, _ := f.store.Transcript(ctx, s.ID())
	var injected string
	for _, m := range msgs {
		if m.Role == storage.RoleTool && m.ToolCallID == "w1" {
			injected = m.Content
		}
	}
	if !strings.Contains(injected, "执行结果未知") {
		t.Fatalf("injected result = %q", injected)
	}
}

// TestCrashDuringRunningToolMarksUnknownThenResume walks the real startup
// recovery path: a session killed while a tool was running is flipped to
// interrupted, its in-flight call to unknown, and a fresh manager (as after
// restart) resumes it with an explicit unknown-outcome result.
func TestCrashDuringRunningToolMarksUnknownThenResume(t *testing.T) {
	f := newFixture(t)
	s := f.newSession(t)
	ctx := context.Background()
	user := storage.Message{ID: storage.NewID(), SessionID: s.ID(), Role: storage.RoleUser, Content: "写文件", TurnID: 1}
	if err := f.store.AppendMessage(ctx, &user); err != nil {
		t.Fatal(err)
	}
	call := toolCall("w1", "write_file", map[string]any{"path": "a.txt", "content": "x"})
	assistant := storage.Message{
		ID: storage.NewID(), SessionID: s.ID(), Role: storage.RoleAssistant,
		ToolCallsJSON: storage.MustJSON([]model.ToolCall{call}), TurnID: 1,
	}
	if err := f.store.AppendMessage(ctx, &assistant); err != nil {
		t.Fatal(err)
	}
	if err := f.store.AppendToolCall(ctx, &storage.ToolCallRecord{
		ID: "tc1", SessionID: s.ID(), TurnID: 1, Name: "write_file", ToolRef: "builtin://write_file",
		ArgsJSON: string(call.Arguments), Status: "running",
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.UpdateSessionState(ctx, s.ID(), StateRunningTool); err != nil {
		t.Fatal(err)
	}

	marked, err := f.store.MarkInterruptedNonTerminal(ctx)
	if err != nil || marked != 1 {
		t.Fatalf("MarkInterruptedNonTerminal = %d, %v", marked, err)
	}
	calls, err := f.store.MarkRunningToolCallsUnknown(ctx)
	if err != nil || calls != 1 {
		t.Fatalf("MarkRunningToolCallsUnknown = %d, %v", calls, err)
	}

	restarted := NewManager(f.store, f.prov, f.emitter, nil, Options{DataDir: f.mgr.opts.DataDir})
	s2, err := restarted.GetSession(ctx, s.ID())
	if err != nil {
		t.Fatal(err)
	}
	if s2.State() != StateInterrupted {
		t.Fatalf("rehydrated state = %s", s2.State())
	}
	f.prov.setScript([]model.Chunk{{Delta: "继续完成。"}, {Done: true}})
	if err := s2.Resume(ctx); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	waitResumed(t, s2)
	waitTerminal(t, f, s2)
	if s2.State() != StateCompleted {
		t.Fatalf("state = %s", s2.State())
	}
	msgs, _ := f.store.Transcript(ctx, s.ID())
	var injected string
	for _, m := range msgs {
		if m.Role == storage.RoleTool && m.ToolCallID == "w1" {
			injected = m.Content
		}
	}
	if !strings.Contains(injected, "执行结果未知") {
		t.Fatalf("injected result = %q", injected)
	}
}

func TestRecoveryDiscard(t *testing.T) {
	f := newFixture(t)
	s := f.newSession(t)
	s.setState(StateInterrupted)
	if err := s.Discard(context.Background()); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	waitState(t, s, StateCancelled)
	if n := f.emitter.count(EventDone); n != 1 {
		t.Fatalf("done events = %d", n)
	}
}
