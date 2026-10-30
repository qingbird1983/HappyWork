package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/happywork/happywork/internal/model"
	"github.com/happywork/happywork/internal/permission"
	"github.com/happywork/happywork/internal/storage"
	"github.com/happywork/happywork/internal/tool"
)

// TestTextlessToolCallPersistsAssistant reproduces the real DeepSeek failure:
// an assistant turn that contains only tool_calls (no text) must still be
// persisted before its tool results, otherwise the next provider request has a
// tool message without a preceding tool_calls message.
func TestTextlessToolCallPersistsAssistant(t *testing.T) {
	f := newFixture(t)
	s := f.newSession(t)
	args, _ := json.Marshal(map[string]string{"path": "."})
	f.prov.setPhases(
		[]model.Chunk{
			{ToolCall: &model.ToolCall{ID: "c1", Name: "list_dir", Arguments: args}},
			{Done: true},
		},
		[]model.Chunk{{Delta: "完成"}, {Done: true}},
	)
	f.runSend(t, s, "列目录")
	msgs, err := f.store.Transcript(context.Background(), s.ID())
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) < 4 {
		t.Fatalf("transcript = %+v", msgs)
	}
	if msgs[1].Role != storage.RoleAssistant || !strings.Contains(msgs[1].ToolCallsJSON, `"c1"`) {
		t.Fatalf("assistant tool_calls message missing: %+v", msgs)
	}
	if msgs[2].Role != storage.RoleTool || msgs[2].ToolCallID != "c1" {
		t.Fatalf("tool result order wrong: %+v", msgs)
	}
}

// TestPersistentRuleLifecycle drives the full A3 path: phrase validation,
// cross-session reuse, and revocation.
func TestPersistentRuleLifecycle(t *testing.T) {
	f := newFixture(t)
	s1 := f.newSession(t)
	call := toolCall("w1", "write_file", map[string]any{"path": "a.txt", "content": "first"})
	f.prov.setPhases(writePhases(call)...)
	if err := s1.Send(context.Background(), "写文件"); err != nil {
		t.Fatal(err)
	}
	payload := waitApproval(t, f.emitter, 1)
	if !hasScope(payload, "persistent") {
		t.Fatalf("write approval must offer persistent scope: %+v", payload.ScopeOptions)
	}
	if err := s1.Approve(payload.ApprovalID, "persistent", "随便"); err == nil {
		t.Fatal("wrong phrase must be rejected")
	}
	if err := s1.Approve(payload.ApprovalID, "persistent", PersistentPhrase); err != nil {
		t.Fatalf("persistent approve: %v", err)
	}
	waitTerminal(t, f, s1)
	if body, err := os.ReadFile(filepath.Join(f.root.String(), "a.txt")); err != nil || string(body) != "first" {
		t.Fatalf("file = %q, %v", body, err)
	}
	rules, err := f.mgr.ListRules(context.Background())
	if err != nil || len(rules) != 1 {
		t.Fatalf("rules = %+v, %v", rules, err)
	}
	wantKey := permission.WriteMatchKey(f.root.String())
	if rules[0].MatchKey != wantKey || rules[0].ToolRef != "builtin://write_file" {
		t.Fatalf("rule = %+v, want key %q", rules[0], wantKey)
	}

	// A new session in the same workspace is granted without a dialog.
	s2 := f.newSession(t)
	call2 := toolCall("w2", "write_file", map[string]any{"path": "b.txt", "content": "second"})
	f.prov.setPhases(writePhases(call2)...)
	f.runSend(t, s2, "再写一个")
	if f.emitter.count(EventApprovalRequest) != 1 {
		t.Fatalf("persistent rule did not skip the dialog")
	}
	if body, err := os.ReadFile(filepath.Join(f.root.String(), "b.txt")); err != nil || string(body) != "second" {
		t.Fatalf("second file = %q, %v", body, err)
	}
	verdicts := f.emitter.all(EventPermission)
	found := false
	for _, v := range verdicts {
		p, ok := v.(PermissionPayload)
		if ok && p.Decision == "allow" && p.GrantedBy == permission.ScopePersistent {
			found = true
		}
	}
	if !found {
		t.Fatalf("no persistent-grant permission event: %+v", verdicts)
	}

	// Revocation takes effect immediately for the next check.
	if err := f.mgr.DeleteRule(context.Background(), rules[0].ID); err != nil {
		t.Fatal(err)
	}
	s3 := f.newSession(t)
	call3 := toolCall("w3", "write_file", map[string]any{"path": "c.txt", "content": "third"})
	f.prov.setPhases(writePhases(call3)...)
	if err := s3.Send(context.Background(), "再写"); err != nil {
		t.Fatal(err)
	}
	payload3 := waitApproval(t, f.emitter, 2)
	if err := s3.Deny(payload3.ApprovalID, ""); err != nil {
		t.Fatal(err)
	}
	waitTerminal(t, f, s3)
	if _, err := os.Stat(filepath.Join(f.root.String(), "c.txt")); !os.IsNotExist(err) {
		t.Fatalf("file written after revocation: %v", err)
	}
}

func hasScope(payload ApprovalPayload, scope string) bool {
	for _, o := range payload.ScopeOptions {
		if o.Scope == scope {
			return true
		}
	}
	return false
}

// TestReadonlyPresetDeniesWrite is the A2 integration path: the stored preset
// reaches the engine and denies before any dialog.
func TestReadonlyPresetDeniesWrite(t *testing.T) {
	f := newFixture(t)
	sess, err := f.mgr.CreateSession(context.Background(), f.root.String(), "readonly")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Preset != "readonly" {
		t.Fatalf("preset = %q", sess.Preset)
	}
	s, err := f.mgr.GetSession(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	call := toolCall("w1", "write_file", map[string]any{"path": "a.txt", "content": "x"})
	f.prov.setPhases(writePhases(call)...)
	f.runSend(t, s, "写文件")
	if f.emitter.count(EventApprovalRequest) != 0 {
		t.Fatalf("readonly preset opened a dialog")
	}
	if _, err := os.Stat(filepath.Join(f.root.String(), "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("readonly preset wrote the file")
	}
	msgs, _ := f.store.Transcript(context.Background(), s.ID())
	for _, m := range msgs {
		if m.Role == storage.RoleTool && strings.Contains(m.Content, "权限拒绝") {
			return
		}
	}
	t.Fatalf("deny result missing: %+v", msgs)
}

func TestUnknownPresetRejected(t *testing.T) {
	f := newFixture(t)
	if _, err := f.mgr.CreateSession(context.Background(), f.root.String(), "nope"); err == nil {
		t.Fatal("unknown preset accepted")
	}
}

// TestUntrustedContentForcesOnce covers A7: after an MCP result the same run's
// write is forced to once scope with a trust warning, even though the write row
// normally offers session scope.
func TestUntrustedContentForcesOnce(t *testing.T) {
	f := newFixture(t)
	s := f.newSession(t)
	s.tools.SetExternal(func() []tool.Tool { return []tool.Tool{&fakeMCPTool{}} })

	mcpCall := toolCall("m1", "mcp__demo__echo", map[string]any{"text": "hi"})
	writeCall := toolCall("w1", "write_file", map[string]any{"path": "a.txt", "content": "x"})
	f.prov.setPhases(writePhases(mcpCall, writeCall)...)
	if err := s.Send(context.Background(), "先调用外部工具再写文件"); err != nil {
		t.Fatal(err)
	}
	first := waitApproval(t, f.emitter, 1)
	if err := s.Approve(first.ApprovalID, "once"); err != nil {
		t.Fatal(err)
	}
	second := waitApproval(t, f.emitter, 2)
	if len(second.ScopeOptions) != 1 || second.ScopeOptions[0].Scope != "once" {
		t.Fatalf("untrusted content must force once: %+v", second.ScopeOptions)
	}
	if second.ContentTrust != "untrusted" || second.TrustWarning == "" {
		t.Fatalf("trust warning missing: %+v", second)
	}
	if err := s.Deny(second.ApprovalID, ""); err != nil {
		t.Fatal(err)
	}
	waitTerminal(t, f, s)
}

// TestApprovalRevalidatesTarget covers A7's path-substitution defence: the
// approved absolute path is re-resolved and refused when its identity changed.
func TestApprovalRevalidatesTarget(t *testing.T) {
	f := newFixture(t)
	s := f.newSession(t)
	sub := filepath.Join(f.root.String(), "sub")
	other := filepath.Join(f.root.String(), "other")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	call := toolCall("w1", "write_file", map[string]any{"path": "sub/a.txt", "content": "x"})
	f.prov.setPhases(writePhases(call)...)
	if err := s.Send(context.Background(), "写文件"); err != nil {
		t.Fatal(err)
	}
	payload := waitApproval(t, f.emitter, 1)

	// Replace the approved directory with a symlink to another directory; the
	// absolute path still exists but now denotes a different target.
	if err := os.Rename(sub, filepath.Join(f.root.String(), "sub-old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, sub); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := s.Approve(payload.ApprovalID, "once"); err != nil {
		t.Fatal(err)
	}
	waitTerminal(t, f, s)
	if _, err := os.Stat(filepath.Join(other, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("write followed the substituted path")
	}
	msgs, _ := f.store.Transcript(context.Background(), s.ID())
	for _, m := range msgs {
		if m.Role == storage.RoleTool && strings.Contains(m.Content, "执行目标在审批后发生变化") {
			return
		}
	}
	t.Fatalf("substitution denial missing: %+v", msgs)
}
