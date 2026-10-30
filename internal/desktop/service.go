package desktop

import (
	"context"

	"github.com/happywork/happywork/internal/agent"
	"github.com/happywork/happywork/internal/storage"
)

// SessionService is the frontend-facing API. Method names are addressed from
// JS as desktop.SessionService.<Method> via generated bindings.
type SessionService struct {
	mgr *agent.Manager
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

// Transcript returns the full ordered message list of a session.
func (s *SessionService) Transcript(sessionID string) ([]storage.Message, error) {
	return s.mgr.Store().Transcript(context.Background(), sessionID)
}
