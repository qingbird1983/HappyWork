package desktop

import (
	"context"

	"github.com/happywork/happywork/internal/agent"
	"github.com/happywork/happywork/internal/storage"
)

// AppInfo is the read-only startup metadata shown in the top bar. The data
// destination is stated explicitly: a cloud provider sees workspace content.
type AppInfo struct {
	Provider        string `json:"provider"`
	Model           string `json:"model"`
	Mock            bool   `json:"mock"`
	DataDestination string `json:"dataDestination"`
	MCPServers      int    `json:"mcpServers"`
	MCPError        string `json:"mcpError,omitempty"`
}

// SessionService is the frontend-facing API. Method names are addressed from
// JS as desktop.SessionService.<Method> via generated bindings.
type SessionService struct {
	mgr  *agent.Manager
	info AppInfo
}

// ListSessions returns persisted sessions, most recent first.
func (s *SessionService) ListSessions() ([]storage.Session, error) {
	return s.mgr.ListSessions(context.Background())
}

// CreateSession pins the workspace root and returns the new session.
func (s *SessionService) CreateSession(workspaceRoot string) (storage.Session, error) {
	return s.mgr.CreateSession(context.Background(), workspaceRoot)
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

// Approve accepts a pending approval. scope is "once" or "session".
func (s *SessionService) Approve(sessionID, approvalID, scope string) error {
	sess, err := s.mgr.GetSession(context.Background(), sessionID)
	if err != nil {
		return err
	}
	return sess.Approve(approvalID, scope)
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
