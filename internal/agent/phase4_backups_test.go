package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/happywork/happywork/internal/audit"
	"github.com/happywork/happywork/internal/rollback"
)

// approveWrite drives one write_file call through the approval dialog.
// `nth` is the ordinal of the approval request expected from the emitter.
func approveWrite(t *testing.T, f *fixture, s *Session, nth int, path, content string) {
	t.Helper()
	call := toolCall(fmt.Sprintf("w%d", nth), "write_file", map[string]any{"path": path, "content": content})
	f.prov.setPhases(writePhases(call)...)
	if err := s.Send(context.Background(), fmt.Sprintf("写 %s", path)); err != nil {
		t.Fatalf("Send: %v", err)
	}
	payload := waitApproval(t, f.emitter, nth)
	if err := s.Approve(payload.ApprovalID, "once"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	waitTerminal(t, f, s)
}

func readWorkspaceFile(t *testing.T, f *fixture, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.root.String(), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

func findBackup(t *testing.T, groups []rollback.BackupGroup, rel, kind, reason string) rollback.Backup {
	t.Helper()
	for _, g := range groups {
		if g.RelPath != rel {
			continue
		}
		for _, b := range g.Backups {
			if b.Kind == kind && b.Reason == reason {
				return b
			}
		}
	}
	t.Fatalf("no %s/%s backup for %s: %+v", kind, reason, rel, groups)
	return rollback.Backup{}
}

func auditHas(t *testing.T, f *fixture, s *Session, action, decision string) {
	t.Helper()
	rows, err := f.store.ListAudit(context.Background(), s.ID())
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	for _, r := range rows {
		if r.Action == action && r.Decision == decision {
			return
		}
	}
	t.Fatalf("audit has no %s/%s: %+v", action, decision, rows)
}

// TestPhase4RestoreFlowWithSafetyCopy covers A3/A4 at the session API level:
// restore first copies the current content, and the safety copy can itself be
// restored again.
func TestPhase4RestoreFlowWithSafetyCopy(t *testing.T) {
	f := newFixture(t)
	s := f.newSession(t)
	ctx := context.Background()

	approveWrite(t, f, s, 1, "a.txt", "v1")
	approveWrite(t, f, s, 2, "a.txt", "v2")
	if got := readWorkspaceFile(t, f, "a.txt"); got != "v2" {
		t.Fatalf("after overwrite = %q", got)
	}

	groups, err := f.mgr.ListBackups(ctx, s.ID())
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	first := findBackup(t, groups, "a.txt", rollback.KindOverwrite, rollback.ReasonWrite)
	if first.BackupSHA256 == "" || first.VerifiedAt == "" {
		t.Fatalf("overwrite backup is not verified: %+v", first)
	}

	out, err := f.mgr.RestoreBackup(ctx, s.ID(), first.ID)
	if err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	if !out.Restored || out.SafetyBackupID == "" {
		t.Fatalf("restore outcome = %+v", out)
	}
	if got := readWorkspaceFile(t, f, "a.txt"); got != "v1" {
		t.Fatalf("after restore = %q, want v1", got)
	}
	auditHas(t, f, s, audit.ActionBackup, audit.DecisionRestore)

	// The safety copy holds v2, so restoring it again must undo the restore.
	second, err := f.mgr.RestoreBackup(ctx, s.ID(), out.SafetyBackupID)
	if err != nil {
		t.Fatalf("restore safety copy: %v", err)
	}
	if !second.Restored {
		t.Fatalf("safety restore outcome = %+v", second)
	}
	if got := readWorkspaceFile(t, f, "a.txt"); got != "v2" {
		t.Fatalf("after restoring safety copy = %q, want v2", got)
	}

	rows, err := f.store.ListAudit(ctx, s.ID())
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	restores := 0
	for _, r := range rows {
		if r.Action == audit.ActionBackup && r.Decision == audit.DecisionRestore {
			restores++
		}
	}
	if restores < 2 {
		t.Fatalf("expected two restore audits, got %d", restores)
	}
}

// TestPhase4RestoreRejectedWhileRunning pins the rule that restore/undo never
// race with the actor: an in-flight run must be rejected with ErrBusy.
func TestPhase4RestoreRejectedWhileRunning(t *testing.T) {
	f := newFixture(t)
	s := f.newSession(t)
	ctx := context.Background()

	approveWrite(t, f, s, 1, "a.txt", "v1")
	approveWrite(t, f, s, 2, "a.txt", "v2")
	groups, err := f.mgr.ListBackups(ctx, s.ID())
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	target := findBackup(t, groups, "a.txt", rollback.KindOverwrite, rollback.ReasonWrite)

	// Second run parks in the approval dialog; restore must be refused there.
	call := toolCall("w3", "write_file", map[string]any{"path": "a.txt", "content": "v3"})
	f.prov.setPhases(writePhases(call)...)
	if err := s.Send(ctx, "再写一次"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	payload := waitApproval(t, f.emitter, 3)
	if _, err := f.mgr.RestoreBackup(ctx, s.ID(), target.ID); !errors.Is(err, ErrBusy) {
		t.Fatalf("restore while running err = %v, want ErrBusy", err)
	}
	if _, err := f.mgr.UndoCreatedFile(ctx, s.ID(), target.ID); !errors.Is(err, ErrBusy) {
		t.Fatalf("undo while running err = %v, want ErrBusy", err)
	}
	if err := s.Deny(payload.ApprovalID, "验收取消"); err != nil {
		t.Fatalf("Deny: %v", err)
	}
	waitTerminal(t, f, s)
	if got := readWorkspaceFile(t, f, "a.txt"); got != "v2" {
		t.Fatalf("rejected write changed the file: %q", got)
	}

	// Idle again: the same restore now succeeds.
	if _, err := f.mgr.RestoreBackup(ctx, s.ID(), target.ID); err != nil {
		t.Fatalf("restore when idle: %v", err)
	}
	if got := readWorkspaceFile(t, f, "a.txt"); got != "v1" {
		t.Fatalf("after idle restore = %q, want v1", got)
	}
}

// TestPhase4UndoCreatedFileMovesToBackupArea covers the "created files are
// undone, never unlinked" rule (ADR-0016).
func TestPhase4UndoCreatedFileMovesToBackupArea(t *testing.T) {
	f := newFixture(t)
	s := f.newSession(t)
	ctx := context.Background()

	approveWrite(t, f, s, 1, "new.txt", "新建内容")
	groups, err := f.mgr.ListBackups(ctx, s.ID())
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	created := findBackup(t, groups, "new.txt", rollback.KindCreate, rollback.ReasonWrite)
	if created.WrittenSHA256 == "" {
		t.Fatalf("create record has no written hash: %+v", created)
	}

	out, err := f.mgr.UndoCreatedFile(ctx, s.ID(), created.ID)
	if err != nil {
		t.Fatalf("UndoCreatedFile: %v", err)
	}
	if !out.Restored || out.AlreadyAbsent {
		t.Fatalf("undo outcome = %+v", out)
	}
	if _, err := os.Stat(filepath.Join(f.root.String(), "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("created file still in workspace")
	}
	rec, err := f.store.GetFileBackup(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetFileBackup: %v", err)
	}
	if rec.DiscardedRel == "" {
		t.Fatalf("undo did not record the discarded path")
	}
	trash := filepath.Join(f.mgr.opts.DataDir, "backups", filepath.FromSlash(rec.DiscardedRel))
	data, err := os.ReadFile(trash)
	if err != nil {
		t.Fatalf("discarded file is not readable: %v", err)
	}
	if string(data) != "新建内容" {
		t.Fatalf("discarded content = %q", data)
	}
	auditHas(t, f, s, audit.ActionBackup, audit.DecisionUndoCreate)
}

// TestPhase4ExportAndVerifyChain checks the desktop-facing evidence path:
// a chain written by the agent verifies, exports, and the export is recomputable.
func TestPhase4ExportAndVerifyChain(t *testing.T) {
	f := newFixture(t)
	s := f.newSession(t)
	ctx := context.Background()

	approveWrite(t, f, s, 1, "a.txt", "v1")

	page, err := f.mgr.ListAudit(ctx, s.ID(), 100)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if page.Total == 0 || !page.Verify.OK || page.Verify.SealedRows == 0 {
		t.Fatalf("audit page = %+v", page)
	}
	if len(page.Entries) == 0 || page.Entries[len(page.Entries)-1].EntryHash == "" {
		t.Fatalf("entries are not sealed: %+v", page.Entries)
	}

	res, err := f.mgr.ExportAudit(ctx)
	if err != nil {
		t.Fatalf("ExportAudit: %v", err)
	}
	if res.Rows != page.Total || res.JSONLPath == "" || res.ManifestPath == "" {
		t.Fatalf("export result = %+v", res)
	}
	jsonl, err := os.ReadFile(res.JSONLPath)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	lines := 0
	for _, l := range strings.Split(string(jsonl), "\n") {
		if strings.TrimSpace(l) != "" {
			lines++
		}
	}
	if lines != res.Rows {
		t.Fatalf("export has %d lines, want %d", lines, res.Rows)
	}
	// Evidence is metadata-only: neither the workspace location nor the file
	// body may appear in the export. Compare with separators stripped and
	// ignoring case, because match keys are lower-cased on Windows and the
	// path is backslash-escaped one extra level inside the JSON detail string;
	// a byte-exact check passes locally yet leaks on the Linux CI runner.
	flatten := func(s string) string {
		return strings.ToLower(strings.NewReplacer(`\`, "", `/`, "").Replace(s))
	}
	body := flatten(string(jsonl))
	if needle := flatten(f.root.String()); needle != "" && strings.Contains(body, needle) {
		t.Fatalf("export leaked workspace path %q: %s", f.root.String(), jsonl)
	}
	if strings.Contains(body, "v1") {
		t.Fatalf("export leaked file content: %s", jsonl)
	}
	if _, err := os.Stat(res.ManifestPath); err != nil {
		t.Fatalf("manifest missing: %v", err)
	}
}
