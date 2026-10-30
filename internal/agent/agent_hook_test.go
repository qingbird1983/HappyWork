package agent

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/happywork/happywork/internal/hook"
	"github.com/happywork/happywork/internal/model"
	"github.com/happywork/happywork/internal/storage"
	"github.com/happywork/happywork/internal/workspace"
)

type testHook struct {
	name   string
	points []hook.Point
	handle func(context.Context, *hook.Event) (hook.Outcome, error)
}

func (h *testHook) Name() string         { return h.name }
func (h *testHook) Points() []hook.Point { return h.points }
func (h *testHook) Priority() int        { return 10 }
func (h *testHook) Handle(ctx context.Context, ev *hook.Event) (hook.Outcome, error) {
	return h.handle(ctx, ev)
}

type hookAudit struct {
	mu   sync.Mutex
	rows []string
}

func (a *hookAudit) record(_ hook.Event, name, kind, reason string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rows = append(a.rows, name+"|"+kind+"|"+reason)
}

func (a *hookAudit) contains(sub string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, row := range a.rows {
		if strings.Contains(row, sub) {
			return true
		}
	}
	return false
}

func newHookFixture(t *testing.T, entries []hook.Entry, audit hook.AuditFunc) *fixture {
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
	mgr := NewManager(store, prov, em, log.New(os.Stderr, "test ", 0), Options{
		DataDir: dir,
		Hooks:   hook.NewRunner(entries, audit),
	})
	return &fixture{mgr: mgr, emitter: em, store: store, root: root, prov: prov}
}

// TestHookPatchesWriteAndAudits drives the full pipeline: the model asks for
// one content, the hook rewrites it, the approval preview and the file show
// the final value, and the audit records only field/value hashes.
func TestHookPatchesWriteAndAudits(t *testing.T) {
	audit := &hookAudit{}
	patchHook := &testHook{name: "rewriter", points: []hook.Point{hook.BeforeToolCall},
		handle: func(context.Context, *hook.Event) (hook.Outcome, error) {
			return hook.Outcome{Patches: []hook.Patch{{Field: "content", Value: "来自 Hook 的内容"}}}, nil
		}}
	f := newHookFixture(t, []hook.Entry{{Hook: patchHook}}, audit.record)
	s := f.newSession(t)
	call := toolCall("w1", "write_file", map[string]any{"path": "a.txt", "content": "模型内容"})
	f.prov.setPhases(writePhases(call)...)

	if err := s.Send(context.Background(), "写文件"); err != nil {
		t.Fatal(err)
	}
	payload := waitApproval(t, f.emitter, 1)
	if !strings.Contains(payload.Preview, "来自 Hook 的内容") {
		t.Fatalf("preview must show the patched content: %q", payload.Preview)
	}
	if strings.Contains(payload.Preview, "模型内容") {
		t.Fatalf("preview still shows the original content: %q", payload.Preview)
	}
	if err := s.Approve(payload.ApprovalID, "once"); err != nil {
		t.Fatal(err)
	}
	waitTerminal(t, f, s)
	body, err := os.ReadFile(filepath.Join(f.root.String(), "a.txt"))
	if err != nil || string(body) != "来自 Hook 的内容" {
		t.Fatalf("file = %q, %v", body, err)
	}
	if !audit.contains("field=content") || !audit.contains(hook.KindPatch) {
		t.Fatalf("patch audit missing: %v", audit.rows)
	}
	for _, row := range audit.rows {
		if strings.Contains(row, "来自 Hook 的内容") || strings.Contains(row, "模型内容") {
			t.Fatalf("audit leaked content: %q", row)
		}
	}
}

func TestHookBeforeToolDeniesWithoutApproval(t *testing.T) {
	audit := &hookAudit{}
	denyHook := &testHook{name: "denier", points: []hook.Point{hook.BeforeToolCall},
		handle: func(context.Context, *hook.Event) (hook.Outcome, error) {
			return hook.Outcome{DenyReason: "测试拒绝"}, nil
		}}
	f := newHookFixture(t, []hook.Entry{{Hook: denyHook}}, audit.record)
	s := f.newSession(t)
	call := toolCall("w1", "write_file", map[string]any{"path": "a.txt", "content": "x"})
	f.prov.setPhases(writePhases(call)...)

	f.runSend(t, s, "写文件")
	if f.emitter.count(EventApprovalRequest) != 0 {
		t.Fatalf("hook deny must not open a dialog")
	}
	if _, err := os.Stat(filepath.Join(f.root.String(), "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("file written despite hook deny: %v", err)
	}
	if !audit.contains(hook.KindDeny) {
		t.Fatalf("deny audit missing: %v", audit.rows)
	}
	msgs, _ := f.store.Transcript(context.Background(), s.ID())
	found := false
	for _, m := range msgs {
		if m.Role == storage.RoleTool && strings.Contains(m.Content, "Hook 拒绝执行") {
			found = true
		}
	}
	if !found {
		t.Fatalf("tool result does not explain the hook deny: %+v", msgs)
	}
}

func TestHookBeforeApprovalDeniesWithoutDialog(t *testing.T) {
	audit := &hookAudit{}
	denyHook := &testHook{name: "approval-denier", points: []hook.Point{hook.BeforeApproval},
		handle: func(context.Context, *hook.Event) (hook.Outcome, error) {
			return hook.Outcome{DenyReason: "审批前检查拒绝"}, nil
		}}
	f := newHookFixture(t, []hook.Entry{{Hook: denyHook}}, audit.record)
	s := f.newSession(t)
	call := toolCall("w1", "write_file", map[string]any{"path": "a.txt", "content": "x"})
	f.prov.setPhases(writePhases(call)...)

	f.runSend(t, s, "写文件")
	if f.emitter.count(EventApprovalRequest) != 0 {
		t.Fatalf("before_approval deny must not open a dialog")
	}
	if _, err := os.Stat(filepath.Join(f.root.String(), "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("file written despite before_approval deny")
	}
	if !audit.contains(hook.KindDeny) {
		t.Fatalf("audit = %v", audit.rows)
	}
}

func TestHookBeforeApprovalEscalatesBlock(t *testing.T) {
	escalate := &testHook{name: "escalate", points: []hook.Point{hook.BeforeApproval},
		handle: func(context.Context, *hook.Event) (hook.Outcome, error) {
			return hook.Outcome{EscalateBlock: true, Warnings: []string{"需要额外确认"}}, nil
		}}
	f := newHookFixture(t, []hook.Entry{{Hook: escalate}}, nil)
	s := f.newSession(t)
	call := toolCall("w1", "write_file", map[string]any{"path": "a.txt", "content": "x"})
	f.prov.setPhases(writePhases(call)...)
	if err := s.Send(context.Background(), "写文件"); err != nil {
		t.Fatal(err)
	}
	payload := waitApproval(t, f.emitter, 1)
	if payload.Level != "block" {
		t.Fatalf("level = %q, want block", payload.Level)
	}
	if len(payload.Warnings) == 0 || payload.Warnings[0] != "需要额外确认" {
		t.Fatalf("warnings = %v", payload.Warnings)
	}
	if err := s.Deny(payload.ApprovalID, ""); err != nil {
		t.Fatal(err)
	}
	waitTerminal(t, f, s)
}

func TestHookModelPoints(t *testing.T) {
	var mu sync.Mutex
	var points []hook.Point
	record := &testHook{name: "model", points: []hook.Point{hook.BeforeModelCall, hook.AfterModelCall},
		handle: func(_ context.Context, ev *hook.Event) (hook.Outcome, error) {
			mu.Lock()
			points = append(points, ev.Point)
			mu.Unlock()
			return hook.Outcome{}, nil
		}}
	f := newHookFixture(t, []hook.Entry{{Hook: record}}, nil)
	s := f.newSession(t)
	f.prov.setScript([]model.Chunk{{Delta: "好"}, {Done: true}})
	f.runSend(t, s, "你好")
	if s.State() != StateCompleted {
		t.Fatalf("state = %s", s.State())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(points) != 2 || points[0] != hook.BeforeModelCall || points[1] != hook.AfterModelCall {
		t.Fatalf("points = %v", points)
	}
}

func TestHookBeforeModelDenyEndsRun(t *testing.T) {
	deny := &testHook{name: "model-deny", points: []hook.Point{hook.BeforeModelCall},
		handle: func(context.Context, *hook.Event) (hook.Outcome, error) {
			return hook.Outcome{DenyReason: "模型调用被拦截"}, nil
		}}
	f := newHookFixture(t, []hook.Entry{{Hook: deny}}, nil)
	s := f.newSession(t)
	f.prov.setScript([]model.Chunk{{Delta: "不应出现"}, {Done: true}})
	f.runSend(t, s, "你好")
	if s.State() != StateError {
		t.Fatalf("state = %s, want error", s.State())
	}
	msgs, _ := f.store.Transcript(context.Background(), s.ID())
	for _, m := range msgs {
		if m.Role == storage.RoleAssistant {
			t.Fatalf("assistant output persisted despite before_model deny: %+v", m)
		}
	}
}

func TestHookAfterToolCannotPatchOrDeny(t *testing.T) {
	audit := &hookAudit{}
	after := &testHook{name: "after", points: []hook.Point{hook.AfterToolCall},
		handle: func(context.Context, *hook.Event) (hook.Outcome, error) {
			return hook.Outcome{
				Patches:    []hook.Patch{{Field: "content", Value: "x"}},
				DenyReason: "已发生",
			}, nil
		}}
	f := newHookFixture(t, []hook.Entry{{Hook: after}}, audit.record)
	s := f.newSession(t)
	args, _ := json.Marshal(map[string]string{"path": "in.txt"})
	if err := os.WriteFile(filepath.Join(f.root.String(), "in.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.prov.setPhases(
		[]model.Chunk{{ToolCall: &model.ToolCall{ID: "c1", Name: "read_file", Arguments: args}}, {Done: true}},
		[]model.Chunk{{Delta: "完成"}, {Done: true}},
	)
	f.runSend(t, s, "读")
	if s.State() != StateCompleted {
		t.Fatalf("state = %s", s.State())
	}
	if !audit.contains("rejected") {
		t.Fatalf("after patch rejection not audited: %v", audit.rows)
	}
}
