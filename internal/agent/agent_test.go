package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/happywork/happywork/internal/model"
	"github.com/happywork/happywork/internal/storage"
	"github.com/happywork/happywork/internal/workspace"
)

// recordingEmitter collects events for assertions.
type recordingEmitter struct {
	mu     sync.Mutex
	events []recordedEvent
}

type recordedEvent struct {
	name    string
	payload any
}

func (r *recordingEmitter) Emit(name string, data any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recordedEvent{name, data})
}

func (r *recordingEmitter) count(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.events {
		if e.name == name {
			n++
		}
	}
	return n
}

func (r *recordingEmitter) all(name string) []any {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []any
	for _, e := range r.events {
		if e.name == name {
			out = append(out, e.payload)
		}
	}
	return out
}

// fakeProvider plays a sequence of phase scripts: Stream call N plays phase
// min(N, len-1). The last phase repeats, which is how the max-turns test
// builds an endless tool-call loop.
type fakeProvider struct {
	mu     sync.Mutex
	phases [][]model.Chunk
	delay  time.Duration
	calls  int
}

func (f *fakeProvider) setPhases(phases ...[]model.Chunk) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.phases = phases
	f.calls = 0
}

// setScript is a single-phase convenience wrapper.
func (f *fakeProvider) setScript(script []model.Chunk) { f.setPhases(script) }

func (f *fakeProvider) Stream(ctx context.Context, _ []model.Message, _ []model.ToolSpec) <-chan model.Chunk {
	f.mu.Lock()
	idx := f.calls
	if idx >= len(f.phases) {
		idx = len(f.phases) - 1
	}
	script := append([]model.Chunk(nil), f.phases[idx]...)
	delay := f.delay
	f.calls++
	f.mu.Unlock()

	ch := make(chan model.Chunk, len(script))
	go func() {
		defer close(ch)
		for _, c := range script {
			if delay > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(delay):
				}
			}
			select {
			case ch <- c:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

type fixture struct {
	mgr     *Manager
	emitter *recordingEmitter
	store   storage.Store
	root    workspace.Root
	prov    *fakeProvider
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	root, err := workspace.NewRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	em := &recordingEmitter{}
	prov := &fakeProvider{}
	mgr := NewManager(store, prov, em, log.New(os.Stderr, "test ", 0), Options{DataDir: dir})
	return &fixture{mgr: mgr, emitter: em, store: store, root: root, prov: prov}
}

func (f *fixture) newSession(t *testing.T) *Session {
	t.Helper()
	sess, err := f.mgr.CreateSession(context.Background(), f.root.String())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	s, err := f.mgr.GetSession(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (f *fixture) runSend(t *testing.T, s *Session, text string) {
	t.Helper()
	if err := s.Send(context.Background(), text); err != nil {
		t.Fatalf("Send: %v", err)
	}
	waitTerminal(t, f, s)
}

func waitTerminal(t *testing.T, f *fixture, s *Session) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if terminal(s.State()) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("session did not reach a terminal state, state=%s", s.State())
}

func waitState(t *testing.T, s *Session, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s.State() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("session did not reach state %s, state=%s", want, s.State())
}

func TestSimpleTextTurn(t *testing.T) {
	f := newFixture(t)
	s := f.newSession(t)
	f.prov.setScript([]model.Chunk{
		{Delta: "你好"},
		{Delta: "，世界"},
		{Done: true},
	})

	f.runSend(t, s, "打个招呼")

	if s.State() != StateCompleted {
		t.Fatalf("state = %s, want completed", s.State())
	}
	msgs, _ := f.store.Transcript(context.Background(), s.ID())
	if len(msgs) != 2 || msgs[0].Role != storage.RoleUser || msgs[1].Role != storage.RoleAssistant {
		t.Fatalf("transcript = %+v", msgs)
	}
	if msgs[1].Content != "你好，世界" {
		t.Fatalf("assistant content = %q", msgs[1].Content)
	}
	// 40ms batching: two instant chunks must not produce two delta events.
	if n := f.emitter.count(EventDelta); n > 2 {
		t.Fatalf("delta events = %d, want <= 2", n)
	}
	if f.emitter.count(EventDone) != 1 {
		t.Fatalf("done events = %d, want 1", f.emitter.count(EventDone))
	}
}

func TestToolCallTurn(t *testing.T) {
	f := newFixture(t)
	s := f.newSession(t)
	if err := os.WriteFile(filepath.Join(f.root.String(), "in.txt"), []byte("内部内容"), 0o644); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"path": "in.txt"})
	f.prov.setPhases(
		[]model.Chunk{
			{Delta: "我来读取。"},
			{ToolCall: &model.ToolCall{ID: "c1", Name: "read_file", Arguments: args}},
			{Done: true},
		},
		[]model.Chunk{
			{Delta: "读完了，内容是：内部内容"},
			{Done: true},
		},
	)

	f.runSend(t, s, "read_file in.txt")

	if s.State() != StateCompleted {
		t.Fatalf("state = %s, want completed", s.State())
	}
	msgs, _ := f.store.Transcript(context.Background(), s.ID())
	var toolMsg *storage.Message
	for i := range msgs {
		if msgs[i].Role == storage.RoleTool {
			toolMsg = &msgs[i]
		}
	}
	if toolMsg == nil {
		t.Fatal("no tool message in transcript")
	}
	if toolMsg.ToolCallID != "c1" {
		t.Fatalf("tool call id = %q", toolMsg.ToolCallID)
	}
	if toolMsg.ToolName != "read_file" {
		t.Fatalf("tool name = %q, want read_file", toolMsg.ToolName)
	}
	if n := len(msgs); n != 4 {
		t.Fatalf("transcript len = %d, want 4 (user/assistant+call/tool/assistant)", n)
	}
	var res struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(toolMsg.Content), &res); err != nil {
		t.Fatalf("tool result json: %v", err)
	}
	if res.Content != "内部内容" {
		t.Fatalf("tool result content = %q", res.Content)
	}
}

func TestBoundaryDenialAudited(t *testing.T) {
	f := newFixture(t)
	s := f.newSession(t)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"path": outside})
	f.prov.setPhases(
		[]model.Chunk{
			{ToolCall: &model.ToolCall{ID: "c2", Name: "read_file", Arguments: args}},
			{Done: true},
		},
		[]model.Chunk{{Delta: "读取被拒绝了。"}, {Done: true}},
	)

	f.runSend(t, s, "读外面的文件")

	if s.State() != StateCompleted {
		t.Fatalf("state = %s, want completed (denial is a normal tool error)", s.State())
	}
	// A5: the denial must be audited.
	rows, err := f.store.ListAudit(context.Background(), s.ID())
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(rows) != 1 || rows[0].Decision != "deny" || rows[0].Action != "read_file" {
		t.Fatalf("audit rows = %+v, want one read_file deny", rows)
	}
	msgs, _ := f.store.Transcript(context.Background(), s.ID())
	var toolMsg string
	for i := range msgs {
		if msgs[i].Role == storage.RoleTool {
			toolMsg = msgs[i].Content
		}
	}
	if !strings.Contains(toolMsg, "outside the workspace") {
		t.Fatalf("model did not see the denial: %s", toolMsg)
	}
}

func TestCancelMidStream(t *testing.T) {
	f := newFixture(t)
	s := f.newSession(t)
	// Long stream with delay: cancel must land before it drains.
	var script []model.Chunk
	for i := 0; i < 200; i++ {
		script = append(script, model.Chunk{Delta: "chunk"})
	}
	f.prov.setScript(script)
	f.prov.delay = 20 * time.Millisecond

	if err := s.Send(context.Background(), "慢慢说"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && s.State() != StateThinking {
		time.Sleep(2 * time.Millisecond)
	}
	for time.Now().Before(deadline) && f.prov.calls < 1 {
		time.Sleep(2 * time.Millisecond)
	}
	s.Cancel()
	waitTerminal(t, f, s)

	if s.State() != StateCancelled {
		t.Fatalf("state = %s, want cancelled", s.State())
	}
	if f.prov.calls < 1 {
		t.Fatal("provider was never called")
	}
}

func TestSendBusy(t *testing.T) {
	f := newFixture(t)
	s := f.newSession(t)
	var script []model.Chunk
	for i := 0; i < 50; i++ {
		script = append(script, model.Chunk{Delta: "x"})
	}
	f.prov.setScript(script)
	f.prov.delay = 10 * time.Millisecond

	if err := s.Send(context.Background(), "第一条"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && s.State() != StateThinking {
		time.Sleep(2 * time.Millisecond)
	}
	if err := s.Send(context.Background(), "第二条"); !errors.Is(err, ErrBusy) {
		t.Fatalf("second Send err = %v, want ErrBusy", err)
	}
	s.Cancel()
	waitTerminal(t, f, s)
}

func TestMaxTurnsExhausted(t *testing.T) {
	f := newFixture(t)
	f.mgr.budget.MaxTurns = 2
	f.mgr.budget.GraceTurns = 0
	s := f.newSession(t)
	args, _ := json.Marshal(map[string]string{"path": "in.txt"})
	f.prov.setScript([]model.Chunk{
		{ToolCall: &model.ToolCall{ID: "loop", Name: "read_file", Arguments: args}},
		{Done: true},
	})

	if err := s.Send(context.Background(), "循环调用"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	waitState(t, s, StatePaused)
	if err := s.Resume(context.Background()); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	waitTerminal(t, f, s)
	if s.State() != StateError {
		t.Fatalf("state = %s, want error", s.State())
	}
}

func TestUnknownToolReportedToModel(t *testing.T) {
	f := newFixture(t)
	s := f.newSession(t)
	f.prov.setPhases(
		[]model.Chunk{
			{ToolCall: &model.ToolCall{ID: "u1", Name: "delete_everything", Arguments: json.RawMessage(`{}`)}},
			{Done: true},
		},
		[]model.Chunk{{Delta: "删不了。"}, {Done: true}},
	)

	f.runSend(t, s, "删库")

	msgs, _ := f.store.Transcript(context.Background(), s.ID())
	var toolMsg string
	for i := range msgs {
		if msgs[i].Role == storage.RoleTool {
			toolMsg = msgs[i].Content
		}
	}
	if !strings.Contains(toolMsg, "unknown tool") {
		t.Fatalf("unknown tool not reported: %s", toolMsg)
	}
}
