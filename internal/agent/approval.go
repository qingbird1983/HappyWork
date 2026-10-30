package agent

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrStaleApproval means the approval id is unknown, expired, or already decided.
	ErrStaleApproval = errors.New("agent: approval is stale or unknown")
	// ErrNotResumable means Resume/Discard does not apply to the current state.
	ErrNotResumable = errors.New("agent: session is not resumable in this state")
	// ErrArgsDrift means the tool arguments changed after the approval snapshot.
	ErrArgsDrift = errors.New("agent: tool arguments changed after approval")
	// ErrBudgetExhausted means a budget item exceeded its limit a second time.
	ErrBudgetExhausted = errors.New("agent: budget exhausted after grace")
)

const defaultApprovalTimeout = 5 * time.Minute

// approvalDecision is delivered through Session.approvals.
type approvalDecision struct {
	id       string
	allow    bool
	scope    string // once | session
	feedback string
	snapshot string
}

// pendingApproval tracks the single outstanding request visible to Approve/Deny.
type pendingApproval struct {
	id       string
	snapshot string
	decided  bool
}

// Approve accepts the pending request. scope is "once" or "session"; session
// scope is rejected unless the request offered it.
func (s *Session) Approve(approvalID, scope string) error {
	if scope == "" {
		scope = "once"
	}
	if scope != "once" && scope != "session" {
		return errors.New("agent: approval scope must be once or session")
	}
	s.mu.Lock()
	allowSession := s.pendingAllowSession
	s.mu.Unlock()
	if scope == "session" && !allowSession {
		return errors.New("agent: this request does not offer session scope")
	}
	return s.decide(approvalID, true, scope, "")
}

// Deny rejects the pending request with optional feedback for the model.
func (s *Session) Deny(approvalID, feedback string) error {
	return s.decide(approvalID, false, "once", feedback)
}

func (s *Session) decide(approvalID string, allow bool, scope, feedback string) error {
	s.mu.Lock()
	p := s.pending
	if p == nil || p.id != approvalID || p.decided {
		s.mu.Unlock()
		return ErrStaleApproval
	}
	p.decided = true
	snapshot := p.snapshot
	s.mu.Unlock()
	select {
	case s.approvals <- approvalDecision{id: approvalID, allow: allow, scope: scope, feedback: feedback, snapshot: snapshot}:
		return nil
	default:
		return ErrStaleApproval
	}
}

// awaitApproval blocks until a decision, timeout (paused), or cancellation.
// The same approval id is re-issued after Resume with a new deadline.
func (s *Session) awaitApproval(ctx context.Context, payload ApprovalPayload, snapshot string, allowSession bool) (approvalDecision, error) {
	timeout := s.mgr.approvalTimeout
	if timeout <= 0 {
		timeout = defaultApprovalTimeout
	}
	s.mu.Lock()
	s.pending = &pendingApproval{id: payload.ApprovalID, snapshot: snapshot}
	s.pendingAllowSession = allowSession
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.pending != nil && s.pending.id == payload.ApprovalID {
			s.pending = nil
		}
		s.pendingAllowSession = false
		s.mu.Unlock()
	}()

outer:
	for {
		s.setState(StateAwaitingApproval)
		payload.ExpiresAt = s.clock.Now().Add(timeout).UTC().Format(time.RFC3339)
		s.mgr.emitEvent(s.info.ID, EventApprovalRequest, payload)
		timer := s.clock.After(timeout)
		for {
			select {
			case <-ctx.Done():
				return approvalDecision{}, ctx.Err()
			case <-s.resumeCh:
				// A stray Resume while the dialog is live: keep the same request.
				continue
			case d := <-s.approvals:
				if d.id != payload.ApprovalID {
					continue
				}
				if d.snapshot != snapshot {
					return approvalDecision{}, ErrArgsDrift
				}
				return d, nil
			case <-timer:
				s.setState(StatePaused)
				s.mgr.emitEvent(s.info.ID, EventPaused, PausedPayload{
					SessionID: s.info.ID,
					Reason:    "approval_timeout",
					Detail:    "审批等待超过 " + timeout.String() + "；可继续或取消",
				})
				// Park until the user resumes (re-ask), decides late, or cancels.
				for {
					select {
					case <-ctx.Done():
						return approvalDecision{}, ctx.Err()
					case <-s.resumeCh:
						continue outer
					case d := <-s.approvals:
						if d.id == payload.ApprovalID {
							if d.snapshot != snapshot {
								return approvalDecision{}, ErrArgsDrift
							}
							return d, nil
						}
					}
				}
			}
		}
	}
}
