package agent

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/happywork/happywork/internal/audit"
	"github.com/happywork/happywork/internal/storage"
	"github.com/happywork/happywork/internal/workspace"
)

func TestPhase4AuditCoversWriteApprovalAndBackup(t *testing.T) {
	f := newFixture(t)
	s := f.newSession(t)
	call := toolCall("w1", "write_file", map[string]any{"path": "a.txt", "content": "hello"})
	f.prov.setPhases(writePhases(call)...)
	if err := s.Send(context.Background(), "写一个文件"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	payload := waitApproval(t, f.emitter, 1)
	if err := s.Approve(payload.ApprovalID, "once"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	waitTerminal(t, f, s)

	rows, err := f.store.ListAudit(context.Background(), s.ID())
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	found := map[string]bool{}
	for _, r := range rows {
		switch {
		case r.Action == "tool_call" && r.Decision == "ask":
			found["ask"] = true
		case r.Action == "approval" && r.Decision == "allow":
			found["approval"] = true
		case r.Action == "backup" && r.Decision == "create":
			found["backup"] = true
		}
	}
	for _, want := range []string{"ask", "approval", "backup"} {
		if !found[want] {
			t.Fatalf("audit missing %s: %+v", want, rows)
		}
	}
	verify, err := f.store.VerifyAudit(context.Background())
	if err != nil || !verify.OK || verify.SealedRows < len(rows) {
		t.Fatalf("verify = %+v, %v", verify, err)
	}
}

func TestPhase4AuditFailureBlocksAllow(t *testing.T) {
	dir := t.TempDir()
	root, err := workspace.NewRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	real, err := storage.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = real.Close() })
	store := failingAuditStore{Store: real}

	em := &recordingEmitter{}
	prov := &fakeProvider{}
	mgr := NewManager(store, prov, em, log.New(os.Stderr, "test ", 0), Options{DataDir: dir})
	sess, err := mgr.CreateSession(context.Background(), root.String())
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	s, err := mgr.GetSession(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	call := toolCall("r1", "read_file", map[string]any{"path": "a.txt"})
	prov.setPhases(writePhases(call)...)
	if err := s.Send(context.Background(), "读文件"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	waitTerminal(t, &fixture{mgr: mgr, emitter: em, store: store, root: root, prov: prov}, s)

	msgs, _ := mgr.Store().Transcript(context.Background(), s.ID())
	seen := false
	for _, m := range msgs {
		if m.Role == storage.RoleTool && strings.Contains(m.Content, "审计写入失败") {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("tool transcript did not show audit refusal: %+v", msgs)
	}
	auditRows, _ := mgr.Store().ListAudit(context.Background(), s.ID())
	if len(auditRows) != 0 {
		t.Fatalf("failing audit store unexpectedly wrote rows: %+v", auditRows)
	}
}

type failingAuditStore struct {
	storage.Store
}

func (failingAuditStore) AppendAudit(context.Context, audit.Record) error {
	return errors.New("audit unavailable")
}
