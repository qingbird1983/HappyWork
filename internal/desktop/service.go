package desktop

import (
	"context"

	"github.com/happywork/happywork/internal/agent"
	"github.com/happywork/happywork/internal/audit"
	"github.com/happywork/happywork/internal/rollback"
	"github.com/happywork/happywork/internal/storage"
)

// AppInfo is the read-only startup metadata shown in the top bar. The data
// destination is stated explicitly: a cloud provider sees workspace content.
type AppInfo struct {
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	Mock             bool   `json:"mock"`
	DataDestination  string `json:"dataDestination"`
	MCPServers       int    `json:"mcpServers"`
	MCPError         string `json:"mcpError,omitempty"`
	HooksFingerprint string `json:"hooksFingerprint,omitempty"`
	HooksError       string `json:"hooksError,omitempty"`
	PresetsError     string `json:"presetsError,omitempty"`
}

// SessionService is the frontend-facing API. Method names are addressed from
// JS as desktop.SessionService.<Method> via generated bindings.
type SessionService struct {
	mgr  *agent.Manager
	info AppInfo
}

// PresetInfo is one selectable permission profile.
type PresetInfo struct {
	Name              string `json:"name"`
	Description       string `json:"description"`
	PersistentAllowed bool   `json:"persistentAllowed"`
}

// ListSessions returns persisted sessions, most recent first.
func (s *SessionService) ListSessions() ([]storage.Session, error) {
	return s.mgr.ListSessions(context.Background())
}

// CreateSession pins the workspace root and preset, and returns the session.
func (s *SessionService) CreateSession(workspaceRoot, preset string) (storage.Session, error) {
	return s.mgr.CreateSession(context.Background(), workspaceRoot, preset)
}

// ListPresets returns the available profiles with descriptions.
func (s *SessionService) ListPresets() ([]PresetInfo, error) {
	catalog := s.mgr.Presets()
	out := make([]PresetInfo, 0, len(catalog))
	for _, p := range catalog {
		out = append(out, PresetInfo{
			Name: p.Name, Description: p.Description, PersistentAllowed: p.PersistentAllowed,
		})
	}
	return out, nil
}

// ListRules returns persistent permission rules.
func (s *SessionService) ListRules() ([]storage.PermissionRule, error) {
	return s.mgr.ListRules(context.Background())
}

// DeleteRule revokes one persistent permission rule.
func (s *SessionService) DeleteRule(id string) error {
	return s.mgr.DeleteRule(context.Background(), id)
}

// Send submits a user message; the run proceeds asynchronously and reports
// through events.
func (s *SessionService) Send(sessionID, text string) error {
	sess, err := s.mgr.GetSession(context.Background(), sessionID)
	if err != nil {
		return err
	}
	return sess.Send(context.Background(), text)
}

// Cancel aborts the in-flight run of a session.
func (s *SessionService) Cancel(sessionID string) error {
	sess, err := s.mgr.GetSession(context.Background(), sessionID)
	if err != nil {
		return err
	}
	sess.Cancel()
	return nil
}

// Approve accepts a pending approval. scope is "once", "session" or
// "persistent"; persistent requires the typed phrase.
func (s *SessionService) Approve(sessionID, approvalID, scope, phrase string) error {
	sess, err := s.mgr.GetSession(context.Background(), sessionID)
	if err != nil {
		return err
	}
	return sess.Approve(approvalID, scope, phrase)
}

// Deny rejects a pending approval with optional feedback for the model.
func (s *SessionService) Deny(sessionID, approvalID, feedback string) error {
	sess, err := s.mgr.GetSession(context.Background(), sessionID)
	if err != nil {
		return err
	}
	return sess.Deny(approvalID, feedback)
}

// Resume continues a paused run or an interrupted session.
func (s *SessionService) Resume(sessionID string) error {
	sess, err := s.mgr.GetSession(context.Background(), sessionID)
	if err != nil {
		return err
	}
	return sess.Resume(context.Background())
}

// Discard abandons an interrupted session.
func (s *SessionService) Discard(sessionID string) error {
	sess, err := s.mgr.GetSession(context.Background(), sessionID)
	if err != nil {
		return err
	}
	return sess.Discard(context.Background())
}

// AppInfo returns provider/model and the data destination.
func (s *SessionService) AppInfo() (AppInfo, error) {
	return s.info, nil
}

// Transcript returns the full ordered message list of a session.
func (s *SessionService) Transcript(sessionID string) ([]storage.Message, error) {
	return s.mgr.Store().Transcript(context.Background(), sessionID)
}

// ListAudit returns recent audit rows plus the current chain status.
func (s *SessionService) ListAudit(sessionID string, limit int) (audit.Page, error) {
	return s.mgr.ListAudit(context.Background(), sessionID, limit)
}

// VerifyAudit recomputes the audit hash chain.
func (s *SessionService) VerifyAudit() (audit.VerifyResult, error) {
	return s.mgr.VerifyAudit(context.Background())
}

// ExportAudit writes JSONL + manifest and returns the paths.
func (s *SessionService) ExportAudit() (audit.ExportResult, error) {
	return s.mgr.ExportAudit(context.Background())
}

// ListBackups returns versioned file backups for one session.
func (s *SessionService) ListBackups(sessionID string) ([]rollback.BackupGroup, error) {
	return s.mgr.ListBackups(context.Background(), sessionID)
}

// RestoreBackup restores one backup version after creating a safety copy.
func (s *SessionService) RestoreBackup(sessionID, backupID string) (rollback.RestoreOutcome, error) {
	return s.mgr.RestoreBackup(context.Background(), sessionID, backupID)
}

// UndoCreatedFile moves an unchanged created file into the backup area.
func (s *SessionService) UndoCreatedFile(sessionID, backupID string) (rollback.RestoreOutcome, error) {
	return s.mgr.UndoCreatedFile(context.Background(), sessionID, backupID)
}
