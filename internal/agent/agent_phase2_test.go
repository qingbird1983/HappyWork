package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/happywork/happywork/internal/model"
	"github.com/happywork/happywork/internal/permission"
	"github.com/happywork/happywork/internal/storage"
	"github.com/happywork/happywork/internal/tool"
)

// fakeClock lets tests drive approval timeouts without sleeping.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []fakeTimer
}

type fakeTimer struct {
	at time.Time
	ch chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)}
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch := make(chan time.Time, 1)
	at := f.now.Add(d)
	if !at.After(f.now) {
		ch <- f.now
		return ch
	}
	f.timers = append(f.timers, fakeTimer{at: at, ch: ch})
	return ch
}

func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
	pending := f.timers[:0]
	for _, timer := range f.timers {
		if !timer.at.After(f.now) {
			select {
			case timer.ch <- f.now:
			default:
			}
		} else {
			pending = append(pending, timer)
		}
	}
	f.timers = pending
}

func (f *fakeClock) timerCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.timers)
}

func waitTimer(t *testing.T, clock *fakeClock) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if clock.timerCount() > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("approval timer was never registered")
}

func waitApproval(t *testing.T, em *recordingEmitter, want int) ApprovalPayload {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if em.count(EventApprovalRequest) >= want {
			all := em.all(EventApprovalRequest)
			payload, ok := all[want-1].(ApprovalPayload)
			if !ok {
				t.Fatalf("approval payload type %T", all[want-1])
			}
			return payload
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("approval request %d not emitted", want)
	return ApprovalPayload{}
}

func toolCall(id, name string, args map[string]any) model.ToolCall {
	data, _ := json.Marshal(args)
	return model.ToolCall{ID: id, Name: name, Arguments: data}
}

func writePhases(calls ...model.ToolCall) [][]model.Chunk {
	phases := make([][]model.Chunk, 0, len(calls)+1)
	for _, c := range calls {
		phases = append(phases, []model.Chunk{{ToolCall: &c}, {Done: true}})
	}
	return append(phases, []model.Chunk{{Delta: "完成。"}, {Done: true}})
}

func TestWriteApprovalOnce(t *testing.T) {
	f := newFixture(t)
	s := f.newSession(t)
	call := toolCall("w1", "write_file", map[string]any{"path": "a.txt", "content": "第一版\n第二行"})
	f.prov.setPhases(writePhases(call)...)

	if err := s.Send(context.Background(), "写文件"); err != nil {
		t.Fatal(err)
	}
	payload := waitApproval(t, f.emitter, 1)
	if payload.Title != "写入文件" || !strings.Contains(payload.Summary, "新建 a.txt") {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.Level != "confirm" || len(payload.ScopeOptions) != 3 {
		t.Fatalf("level/options = %s %+v", payload.Level, payload.ScopeOptions)
	}
	if payload.ScopeOptions[2].Scope != "persistent" {
		t.Fatalf("write must offer persistent scope: %+v", payload.ScopeOptions)
	}
	if !strings.Contains(payload.Preview, "第一版") {
		t.Fatalf("preview = %q", payload.Preview)
	}
	if err := s.Approve(payload.ApprovalID, "once"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	waitTerminal(t, f, s)
	if s.State() != StateCompleted {
		t.Fatalf("state = %s", s.State())
	}
	body, err := os.ReadFile(filepath.Join(f.root.String(), "a.txt"))
	if err != nil || string(body) != "第一版\n第二行" {
		t.Fatalf("file = %q, %v", body, err)
	}
	if err := s.Approve(payload.ApprovalID, "once"); !errors.Is(err, ErrStaleApproval) {
		t.Fatalf("second approve err = %v, want ErrStaleApproval", err)
	}
	// Events carry the approval decision.
	if err := s.Send(context.Background(), "再来一次"); err != nil {
		t.Fatal(err)
	}
	// The provider repeats the last phase (final text), so this run completes.
	waitTerminal(t, f, s)
	if f.emitter.count(EventApprovalResolved) != 1 {
		t.Fatalf("resolved events = %d", f.emitter.count(EventApprovalResolved))
	}
}

func TestWriteDenyWithFeedback(t *testing.T) {
	f := newFixture(t)
	s := f.newSession(t)
	call := toolCall("w1", "write_file", map[string]any{"path": "a.txt", "content": "nope"})
	f.prov.setPhases(writePhases(call)...)

	if err := s.Send(context.Background(), "写文件"); err != nil {
		t.Fatal(err)
	}
	payload := waitApproval(t, f.emitter, 1)
	if err := s.Deny(payload.ApprovalID, "文件名不对"); err != nil {
		t.Fatalf("Deny: %v", err)
	}
	waitTerminal(t, f, s)
	if s.State() != StateCompleted {
		t.Fatalf("state = %s", s.State())
	}
	if _, err := os.Stat(filepath.Join(f.root.String(), "a.txt")); !os.IsNotExist(err) {
		t.Fatal("denied write must not happen")
	}
	msgs, _ := f.store.Transcript(context.Background(), s.ID())
	var toolMsg string
	for _, m := range msgs {
		if m.Role == storage.RoleTool {
			toolMsg = m.Content
		}
	}
	if !strings.Contains(toolMsg, "用户拒绝执行") || !strings.Contains(toolMsg, "文件名不对") {
		t.Fatalf("tool result = %s", toolMsg)
	}
	audit, _ := f.store.ListAudit(context.Background(), s.ID())
	foundDeny := false
	for _, row := range audit {
		if row.Action == "approval" && row.Decision == "deny" && strings.Contains(row.Reason, "文件名不对") {
			foundDeny = true
		}
	}
	if !foundDeny {
		t.Fatalf("audit = %+v", audit)
	}
}

func TestWriteSessionScope(t *testing.T) {
	f := newFixture(t)
	s := f.newSession(t)
	c1 := toolCall("w1", "write_file", map[string]any{"path": "a.txt", "content": "a"})
	c2 := toolCall("w2", "write_file", map[string]any{"path": "b.txt", "content": "b"})
	f.prov.setPhases(writePhases(c1, c2)...)

	if err := s.Send(context.Background(), "写两个文件"); err != nil {
		t.Fatal(err)
	}
	payload := waitApproval(t, f.emitter, 1)
	if err := s.Approve(payload.ApprovalID, "session"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	waitTerminal(t, f, s)
	if s.State() != StateCompleted {
		t.Fatalf("state = %s", s.State())
	}
	if n := f.emitter.count(EventApprovalRequest); n != 1 {
		t.Fatalf("approval requests = %d, want 1 (session grant)", n)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(f.root.String(), name)); err != nil {
			t.Fatalf("%s not written: %v", name, err)
		}
	}
	matchKey := permission.WriteMatchKey(f.root.String())
	ok, err := f.store.HasSessionApproval(context.Background(), s.ID(), matchKey)
	if err != nil || !ok {
		t.Fatalf("session grant missing: %v %v", ok, err)
	}
}

func TestApprovalTimeoutPauseResume(t *testing.T) {
	f := newFixture(t)
	clock := newFakeClock()
	f.mgr.clock = clock
	f.mgr.approvalTimeout = 5 * time.Minute
	s := f.newSession(t)
	call := toolCall("w1", "write_file", map[string]any{"path": "a.txt", "content": "x"})
	f.prov.setPhases(writePhases(call)...)

	if err := s.Send(context.Background(), "写文件"); err != nil {
		t.Fatal(err)
	}
	waitApproval(t, f.emitter, 1)
	waitTimer(t, clock)
	clock.Advance(5 * time.Minute)
	waitState(t, s, StatePaused)
	// The paused notice must already be in the event stream by the time a paused
	// state is observable; otherwise the GUI renders a session frozen for no
	// reason. Under parallel load the old order (state first, notice after) made
	// this window visible and the count below read 0.
	pausedEvent := f.emitter.firstIndex(func(name string, _ any) bool { return name == EventPaused })
	pausedState := f.emitter.firstIndex(func(name string, payload any) bool {
		sp, ok := payload.(StatePayload)
		return name == EventState && ok && sp.State == StatePaused
	})
	if pausedEvent < 0 {
		t.Fatal("session is paused but no paused event was emitted")
	}
	if pausedState >= 0 && pausedEvent > pausedState {
		t.Fatalf("paused event (idx %d) must precede the paused state event (idx %d)", pausedEvent, pausedState)
	}
	if n := f.emitter.count(EventPaused); n != 1 {
		t.Fatalf("paused events = %d", n)
	}
	if err := s.Resume(context.Background()); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	payload := waitApproval(t, f.emitter, 2)
	if err := s.Approve(payload.ApprovalID, "once"); err != nil {
		t.Fatalf("Approve after resume: %v", err)
	}
	waitTerminal(t, f, s)
	if s.State() != StateCompleted {
		t.Fatalf("state = %s", s.State())
	}
	if _, err := os.Stat(filepath.Join(f.root.String(), "a.txt")); err != nil {
		t.Fatalf("file not written after resume: %v", err)
	}
}

func TestCancelDuringApproval(t *testing.T) {
	f := newFixture(t)
	s := f.newSession(t)
	call := toolCall("w1", "write_file", map[string]any{"path": "a.txt", "content": "x"})
	f.prov.setPhases(writePhases(call)...)

	if err := s.Send(context.Background(), "写文件"); err != nil {
		t.Fatal(err)
	}
	waitApproval(t, f.emitter, 1)
	s.Cancel()
	waitTerminal(t, f, s)
	if s.State() != StateCancelled {
		t.Fatalf("state = %s", s.State())
	}
	if _, err := os.Stat(filepath.Join(f.root.String(), "a.txt")); !os.IsNotExist(err) {
		t.Fatal("cancelled write must not happen")
	}
	if f.emitter.count(EventApprovalResolved) != 0 {
		t.Fatal("timeout/cancel must not record a deny decision")
	}
	msgs, _ := f.store.Transcript(context.Background(), s.ID())
	answered := false
	for _, m := range msgs {
		if m.Role == storage.RoleTool && m.ToolCallID == "w1" && strings.Contains(m.Content, "取消") {
			answered = true
		}
	}
	if !answered {
		t.Fatal("cancelled tool call must still get a result for transcript coherence")
	}
}

func TestSensitiveReadDenied(t *testing.T) {
	f := newFixture(t)
	s := f.newSession(t)
	if err := os.WriteFile(filepath.Join(f.root.String(), ".env"), []byte("SECRET=1"), 0o644); err != nil {
		t.Fatal(err)
	}
	call := toolCall("r1", "read_file", map[string]any{"path": ".env"})
	f.prov.setPhases(writePhases(call)...)
	f.runSend(t, s, "读 .env")
	if s.State() != StateCompleted {
		t.Fatalf("state = %s", s.State())
	}
	audit, _ := f.store.ListAudit(context.Background(), s.ID())
	if len(audit) == 0 || !strings.Contains(audit[0].Reason, "sensitive") {
		t.Fatalf("audit = %+v", audit)
	}
	msgs, _ := f.store.Transcript(context.Background(), s.ID())
	for _, m := range msgs {
		if m.Role == storage.RoleTool && !strings.Contains(m.Content, "sensitive") {
			t.Fatalf("tool result = %s", m.Content)
		}
	}
}

func TestOversizedResultAttachment(t *testing.T) {
	f := newFixture(t)
	s := f.newSession(t)
	big := strings.Repeat("x", 300<<10)
	if err := os.WriteFile(filepath.Join(f.root.String(), "big.txt"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	call := toolCall("r1", "read_file", map[string]any{"path": "big.txt"})
	f.prov.setPhases(writePhases(call)...)
	f.runSend(t, s, "读大文件")
	if s.State() != StateCompleted {
		t.Fatalf("state = %s", s.State())
	}
	msgs, _ := f.store.Transcript(context.Background(), s.ID())
	var toolMsg string
	for _, m := range msgs {
		if m.Role == storage.RoleTool {
			toolMsg = m.Content
		}
	}
	if !strings.Contains(toolMsg, "attachment") || !strings.Contains(toolMsg, "sha256") {
		t.Fatalf("tool result = %s", toolMsg)
	}
	dir := filepath.Join(f.mgr.opts.DataDir, "attachments", s.ID())
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("attachments = %v, %v; tool result = %s", entries, err, toolMsg)
	}
}

func TestStateTransitionsIncludePersisting(t *testing.T) {
	f := newFixture(t)
	s := f.newSession(t)
	call := toolCall("r1", "list_dir", map[string]any{"path": "."})
	f.prov.setPhases(writePhases(call)...)
	f.runSend(t, s, "列目录")
	// The terminal state becomes visible before setState emits its event; wait
	// for the done event so the assertion is not racing the actor goroutine.
	waitEventCount(t, f.emitter, EventDone, 1)
	states := map[string]bool{}
	for _, e := range f.emitter.all(EventState) {
		if p, ok := e.(StatePayload); ok {
			states[p.State] = true
		}
	}
	for _, want := range []string{StateThinking, StateRunningTool, StatePersisting, StateCompleted} {
		if !states[want] {
			t.Fatalf("state %s not emitted; got %v", want, states)
		}
	}
}

// fakeMCPTool stands in for an external tool snapshot.
type fakeMCPTool struct{ destructive bool }

func (f *fakeMCPTool) Meta() tool.Meta {
	return tool.Meta{
		Name: "mcp__demo__echo", Ref: "mcp://demo/echo",
		Description: "echo", Parameters: json.RawMessage(`{"type":"object"}`),
		Action: "unknown", Source: "mcp:demo", Trust: "untrusted",
		Destructive: f.destructive, Reversible: false, RetrySafe: false,
	}
}

func (f *fakeMCPTool) Exec(context.Context, json.RawMessage) (any, error) {
	return map[string]string{"content": "external result"}, nil
}

func TestMCPToolRequiresApprovalAndStaysUntrusted(t *testing.T) {
	f := newFixture(t)
	s := f.newSession(t)
	s.tools.SetExternal(func() []tool.Tool { return []tool.Tool{&fakeMCPTool{destructive: true}} })
	call := toolCall("m1", "mcp__demo__echo", map[string]any{"text": "hi"})
	f.prov.setPhases(writePhases(call)...)

	if err := s.Send(context.Background(), "调用外部工具"); err != nil {
		t.Fatal(err)
	}
	payload := waitApproval(t, f.emitter, 1)
	if payload.Source != "mcp:demo" || payload.Level != "block" || payload.Reversible {
		t.Fatalf("payload = %+v", payload)
	}
	if len(payload.ScopeOptions) != 1 || payload.ScopeOptions[0].Scope != "once" {
		t.Fatalf("MCP must offer once only: %+v", payload.ScopeOptions)
	}
	if err := s.Approve(payload.ApprovalID, "session"); err == nil {
		t.Fatal("session scope must be rejected for MCP")
	}
	if err := s.Approve(payload.ApprovalID, "once"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	waitTerminal(t, f, s)
	if s.State() != StateCompleted {
		t.Fatalf("state = %s", s.State())
	}
	msgs, _ := f.store.Transcript(context.Background(), s.ID())
	var toolMsg *storage.Message
	for i := range msgs {
		if msgs[i].Role == storage.RoleTool && msgs[i].ToolCallID == "m1" {
			toolMsg = &msgs[i]
		}
	}
	if toolMsg == nil || toolMsg.Trust != storage.TrustUntrusted {
		t.Fatalf("tool message trust = %+v", toolMsg)
	}
	if !strings.Contains(toolMsg.Content, "external result") {
		t.Fatalf("content = %s", toolMsg.Content)
	}
}
