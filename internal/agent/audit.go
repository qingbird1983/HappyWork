package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/happywork/happywork/internal/audit"
	"github.com/happywork/happywork/internal/rollback"
	"github.com/happywork/happywork/internal/storage"
)

func (s *Session) beginTrace() string {
	id := storage.NewID()
	s.mu.Lock()
	s.traceID = id
	s.mu.Unlock()
	return id
}

func (s *Session) trace() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.traceID
}

// auditNow seals one record. Callers that are about to allow an execution must
// treat a non-nil error as a refusal (fail closed).
func (s *Session) auditNow(ctx context.Context, rec audit.Record) error {
	rec.SessionID = s.info.ID
	if rec.TraceID == "" {
		rec.TraceID = s.trace()
	}
	if rec.CreatedAt == "" {
		rec.CreatedAt = storage.Now()
	}
	return s.mgr.store.AppendAudit(ctx, rec)
}

func (s *Session) auditBestEffort(ctx context.Context, rec audit.Record) {
	if err := s.auditNow(ctx, rec); err != nil {
		s.mgr.log.Printf("agent: persist audit: %v", err)
	}
}

func auditDetail(extra map[string]any) string {
	if extra == nil {
		return "{}"
	}
	detail := storage.MustJSON(extra)
	if detail == "null" {
		return "{}"
	}
	return detail
}

// relPath converts an absolute path into its workspace-relative form. Windows
// match keys are lower-cased by permission.WriteMatchKey, so the prefix test is
// case-insensitive there.
func (s *Session) relPath(p string) (string, bool) {
	if p == "" {
		return "", false
	}
	abs := filepath.FromSlash(p)
	if !filepath.IsAbs(abs) {
		return filepath.ToSlash(p), true
	}
	root := s.root.String()
	cmpAbs, cmpRoot := abs, root
	if filepath.Separator == '\\' {
		cmpAbs, cmpRoot = strings.ToLower(abs), strings.ToLower(root)
	}
	switch {
	case cmpAbs == cmpRoot:
		return ".", true
	case strings.HasPrefix(cmpAbs, cmpRoot+string(filepath.Separator)):
		return filepath.ToSlash(abs[len(root)+1:]), true
	default:
		return "", false
	}
}

// auditKey renders a permission match key for evidence. The built-in write key
// embeds an absolute parent directory, but audit records are exportable, so the
// directory is rewritten relative to the workspace and anything outside it
// collapses to a marker (design D2: metadata only, no user directory layout).
func (s *Session) auditKey(key string) string {
	i := strings.LastIndexByte(key, '|')
	if i < 0 {
		return key
	}
	rel, ok := s.relPath(key[i+1:])
	if !ok {
		rel = "<outside>"
	}
	return key[:i+1] + rel
}

// auditPath renders a target path for evidence, never as an absolute location.
func (s *Session) auditPath(p string) string {
	if p == "" {
		return ""
	}
	if rel, ok := s.relPath(p); ok {
		return rel
	}
	return "<outside>"
}

// auditTool records one engine decision. Allow/ask callers must stop on error.
func (s *Session) auditTool(ctx context.Context, turnID int64, toolName, toolRef, decision, reason string, extra map[string]any) error {
	detail := map[string]any{"tool": toolName}
	if toolRef != "" {
		detail["toolRef"] = toolRef
	}
	for k, v := range extra {
		detail[k] = v
	}
	return s.auditNow(ctx, audit.Record{
		TurnID: turnID, Action: audit.ActionToolCall, Decision: decision,
		Reason: reason, Detail: auditDetail(detail),
	})
}

func (s *Session) auditToolBestEffort(ctx context.Context, turnID int64, toolName, toolRef, decision, reason string, extra map[string]any) {
	if err := s.auditTool(ctx, turnID, toolName, toolRef, decision, reason, extra); err != nil {
		s.mgr.log.Printf("agent: persist tool audit: %v", err)
	}
}

func (s *Session) auditApproval(ctx context.Context, turnID int64, decision, reason string, detail map[string]any) error {
	return s.auditNow(ctx, audit.Record{
		TurnID: turnID, Action: audit.ActionApproval, Decision: decision,
		Reason: reason, Detail: auditDetail(detail),
	})
}

func (s *Session) auditPermissionRule(ctx context.Context, turnID int64, decision, reason string, detail map[string]any) error {
	return s.auditNow(ctx, audit.Record{
		TurnID: turnID, Action: audit.ActionPermissionRule, Decision: decision,
		Reason: reason, Detail: auditDetail(detail),
	})
}

func (s *Session) auditBackupCreate(ctx context.Context, turnID int64, out rollback.WriteOutcome) {
	rel := s.auditPath(out.Path)
	s.auditBestEffort(ctx, audit.Record{
		TurnID: turnID, Action: audit.ActionBackup, Decision: audit.DecisionCreate,
		Detail: auditDetail(map[string]any{
			"backupId": out.BackupID, "relPath": rel, "version": out.Version,
			"sha256": out.SHA256, "size": out.BytesWritten, "created": out.Created,
		}),
	})
}

// busy reports whether a restore/undo would race with the actor.
func (s *Session) busy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.state {
	case StateThinking, StateRunningTool, StatePersisting, StateAwaitingApproval, StatePaused:
		return true
	}
	return s.running || s.starting
}

// ListBackups returns version groups for this session.
func (s *Session) ListBackups(ctx context.Context) ([]rollback.BackupGroup, error) {
	if s.backups == nil {
		return nil, fmt.Errorf("agent: backup writer is not configured")
	}
	return s.backups.ListGroups(ctx)
}

// RestoreBackup restores one version; only allowed while the session is idle.
func (s *Session) RestoreBackup(ctx context.Context, backupID string) (rollback.RestoreOutcome, error) {
	if s.busy() {
		return rollback.RestoreOutcome{}, ErrBusy
	}
	if s.backups == nil {
		return rollback.RestoreOutcome{}, fmt.Errorf("agent: backup writer is not configured")
	}
	out, err := s.backups.Restore(ctx, backupID)
	if err != nil {
		s.auditBestEffort(ctx, audit.Record{
			Action: audit.ActionBackup, Decision: audit.DecisionVerifyFail,
			Reason: err.Error(), Detail: auditDetail(map[string]any{"backupId": backupID}),
		})
		return out, err
	}
	s.auditBestEffort(ctx, audit.Record{
		Action: audit.ActionBackup, Decision: audit.DecisionRestore,
		Detail: auditDetail(map[string]any{
			"backupId": out.BackupID, "relPath": out.RelPath, "safetyBackupId": out.SafetyBackupID,
			"created": out.Created, "sha256": out.SHA256,
		}),
	})
	return out, nil
}

// UndoCreatedFile moves an unchanged created file into the backup area.
func (s *Session) UndoCreatedFile(ctx context.Context, backupID string) (rollback.RestoreOutcome, error) {
	if s.busy() {
		return rollback.RestoreOutcome{}, ErrBusy
	}
	if s.backups == nil {
		return rollback.RestoreOutcome{}, fmt.Errorf("agent: backup writer is not configured")
	}
	out, err := s.backups.UndoCreate(ctx, backupID)
	if err != nil {
		s.auditBestEffort(ctx, audit.Record{
			Action: audit.ActionBackup, Decision: audit.DecisionVerifyFail,
			Reason: err.Error(), Detail: auditDetail(map[string]any{"backupId": backupID}),
		})
		return out, err
	}
	s.auditBestEffort(ctx, audit.Record{
		Action: audit.ActionBackup, Decision: audit.DecisionUndoCreate,
		Detail: auditDetail(map[string]any{
			"backupId": out.BackupID, "relPath": out.RelPath, "alreadyAbsent": out.AlreadyAbsent,
		}),
	})
	return out, nil
}
