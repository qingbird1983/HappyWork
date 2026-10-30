package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/happywork/happywork/internal/audit"
	"github.com/happywork/happywork/internal/hook"
	"github.com/happywork/happywork/internal/model"
	"github.com/happywork/happywork/internal/permission"
	"github.com/happywork/happywork/internal/storage"
	"github.com/happywork/happywork/internal/tool"
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
	// ErrPersistentPhrase means the typed confirmation phrase did not match.
	ErrPersistentPhrase = errors.New("agent: persistent approval requires the exact confirmation phrase")
)

// PersistentPhrase is the typed confirmation required to create a persistent
// rule (ADR-0002: broad grants need a deliberate, non-click action).
const PersistentPhrase = "允许持久"

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

// Approve accepts the pending request. scope is once, session or persistent;
// persistent requires the exact typed phrase and a request that offered it.
func (s *Session) Approve(approvalID, scope string, phrase ...string) error {
	if scope == "" {
		scope = "once"
	}
	switch scope {
	case "once", "session", "persistent":
	default:
		return errors.New("agent: approval scope must be once, session or persistent")
	}
	s.mu.Lock()
	allowSession := s.pendingAllowSession
	allowPersistent := s.pendingAllowPersist
	s.mu.Unlock()
	if scope == "session" && !allowSession {
		return errors.New("agent: this request does not offer session scope")
	}
	if scope == "persistent" {
		if !allowPersistent {
			return errors.New("agent: this request does not offer persistent scope")
		}
		if len(phrase) == 0 || strings.TrimSpace(phrase[0]) != PersistentPhrase {
			return ErrPersistentPhrase
		}
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
func (s *Session) awaitApproval(ctx context.Context, payload ApprovalPayload, snapshot string, allowSession, allowPersistent bool) (approvalDecision, error) {
	timeout := s.mgr.approvalTimeout
	if timeout <= 0 {
		timeout = defaultApprovalTimeout
	}
	s.mu.Lock()
	s.pending = &pendingApproval{id: payload.ApprovalID, snapshot: snapshot}
	s.pendingAllowSession = allowSession
	s.pendingAllowPersist = allowPersistent
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.pending != nil && s.pending.id == payload.ApprovalID {
			s.pending = nil
		}
		s.pendingAllowSession = false
		s.pendingAllowPersist = false
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

// askApproval emits the dialog and waits for a decision (or approval timeout →
// paused → Resume). It persists the decided approval row, audits the decision
// fail-closed for an allow, and stores the reusable rule for a persistent scope.
func (s *Session) askApproval(ctx context.Context, call model.ToolCall, meta tool.Meta, args map[string]any, canonical, normalizedPath, matchKey, execHash string, verdict permission.Verdict, hookWarnings []string, recordID string, turnID int64) (decision, note string, err error) {
	sessID := s.info.ID
	// before_approval hooks may add warnings, demand a block-level dialog or
	// deny the call outright; they cannot change arguments.
	approvalRes := s.runHooks(ctx, hook.Event{
		Point: hook.BeforeApproval, SessionID: sessID, TurnID: turnID,
		ToolRef: meta.Ref, ToolName: meta.Name, Action: meta.Action, Trust: meta.Trust,
		Args: args, Warnings: hookWarnings,
	}, nil)
	if approvalRes.Denied() {
		s.auditBestEffort(ctx, audit.Record{
			TurnID: turnID, Action: audit.ActionApproval, Decision: audit.DecisionDeny,
			Reason: "hook: " + approvalRes.DenyReason,
			Detail: auditDetail(map[string]any{
				"toolRef": meta.Ref, "matchKey": matchKey, "execHash": execHash, "scope": "once",
			}),
		})
		return "hook_deny", approvalRes.DenyReason, nil
	}
	level := verdict.Level
	if approvalRes.EscalateBlock {
		level = permission.LevelBlock
	}
	warnings := append(append([]string{}, hookWarnings...), approvalRes.Warnings...)
	desc := describeCall(meta, args, normalizedPath, s.root, meta.Source)
	options := []ApprovalOption{{Scope: "once", Label: "仅本次允许", Description: "只执行这一次"}}
	allowSession := verdict.Scope == permission.ScopeSession || verdict.Scope == permission.ScopePersistent
	if allowSession {
		scopeText := "本会话内"
		if meta.Ref == "builtin://write_file" {
			scopeText = "本会话内写该目录下"
		}
		options = append(options, ApprovalOption{
			Scope:       "session",
			Label:       "本会话内允许",
			Description: scopeText + "不再询问",
		})
	}
	allowPersistent := verdict.PersistentAllowed && matchKey != ""
	if allowPersistent {
		options = append(options, ApprovalOption{
			Scope:       "persistent",
			Label:       "持久允许（需输入短语）",
			Description: "本机所有会话有效；可在权限规则中查看和撤销",
		})
	}
	trustWarning := ""
	if s.contentTrust() == permission.Untrusted {
		trustWarning = "当前回合包含外部不可信内容，本次仅允许一次"
	}
	quotaNotice := ""
	if verdict.QuotaExceeded {
		quotaNotice = "自动放行限额已触发，本次需要你确认"
	}
	payload := ApprovalPayload{
		ApprovalID:   storage.NewID(),
		SessionID:    sessID,
		ToolCallID:   call.ID,
		ToolName:     meta.Name,
		ToolRef:      meta.Ref,
		Level:        level,
		Title:        desc.title,
		Summary:      desc.summary,
		Fields:       desc.fields,
		Preview:      desc.preview,
		ScopeOptions: options,
		Source:       meta.Source,
		Reversible:   meta.Reversible,
		ContentTrust: s.contentTrust(),
		TrustWarning: trustWarning,
		QuotaNotice:  quotaNotice,
		Warnings:     warnings,
		Preset:       s.preset(),
	}
	d, err := s.awaitApproval(ctx, payload, canonical, allowSession, allowPersistent)
	if err != nil {
		return "", "", err
	}
	decision = "deny"
	note = d.feedback
	if d.allow {
		decision = "allow"
	}
	now := storage.Now()
	row := storage.Approval{
		ID:           payload.ApprovalID,
		SessionID:    sessID,
		TurnID:       turnID,
		ToolCallID:   recordID,
		ToolRef:      meta.Ref,
		MatchKey:     matchKey,
		Level:        level,
		Decision:     decision,
		Scope:        d.scope,
		ArgsJSON:     canonical,
		ExecHash:     execHash,
		ContentTrust: s.contentTrust(),
		Note:         note,
		DecidedAt:    now,
	}
	if err := s.mgr.store.AppendApproval(ctx, &row); err != nil {
		s.mgr.log.Printf("agent: persist approval: %v", err)
	}
	if err := s.auditApproval(ctx, turnID, decision, note, map[string]any{
		"approvalID": payload.ApprovalID, "scope": d.scope, "toolRef": meta.Ref,
		"matchKey": matchKey, "execHash": execHash, "contentTrust": s.contentTrust(),
	}); err != nil {
		// A denied request already stopped the tool, so a missing audit row is a
		// logging problem; an allowed one must not run unrecorded.
		if d.allow {
			return "", "", fmt.Errorf("agent: audit approval: %w", err)
		}
		s.mgr.log.Printf("agent: persist approval audit: %v", err)
	}
	if d.allow && d.scope == permission.ScopePersistent && matchKey != "" {
		rule := storage.PermissionRule{
			ToolRef: meta.Ref, Action: meta.Action, MatchKey: matchKey,
			Note: "created from approval " + payload.ApprovalID,
		}
		if err := s.mgr.store.CreatePermissionRule(ctx, &rule); err != nil {
			s.mgr.log.Printf("agent: persist permission rule: %v", err)
			s.notice("warn", "持久规则保存失败，本次批准仍然有效")
		} else {
			s.notice("info", "已创建持久写入规则；可在权限规则中查看和撤销")
			if err := s.auditPermissionRule(ctx, turnID, audit.DecisionCreate,
				"created from approval "+payload.ApprovalID,
				map[string]any{"ruleID": rule.ID, "toolRef": meta.Ref, "matchKey": matchKey}); err != nil {
				s.mgr.log.Printf("agent: persist rule audit: %v", err)
			}
		}
	}
	s.mgr.emitEvent(sessID, EventApprovalResolved, ApprovalResolvedPayload{
		SessionID: sessID, ApprovalID: payload.ApprovalID, Decision: decision, Scope: d.scope,
	})
	if d.allow {
		return decision, "", nil
	}
	return decision, note, nil
}
