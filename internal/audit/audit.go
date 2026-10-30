// Package audit defines the immutable audit record, its canonical hash and
// the chain verifier/export used by Phase 4 (ADR-0015). It is a leaf package:
// it only depends on internal/jcs and the standard library.
package audit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/happywork/happywork/internal/jcs"
)

// Actions recorded by the security pipeline.
const (
	ActionToolCall       = "tool_call"
	ActionApproval       = "approval"
	ActionPermissionRule = "permission_rule"
	ActionHook           = "hook"
	ActionBackup         = "backup"
	ActionAuditVerify    = "audit_verify"
	ActionRecovery       = "recovery"
)

// Decisions used by the actions above.
const (
	DecisionAuto         = "auto"
	DecisionAllow        = "allow"
	DecisionAsk          = "ask"
	DecisionDeny         = "deny"
	DecisionCreate       = "create"
	DecisionHit          = "hit"
	DecisionDelete       = "delete"
	DecisionWarn         = "warn"
	DecisionFailClosed   = "fail_closed"
	DecisionPatch        = "patch"
	DecisionRestore      = "restore"
	DecisionUndoCreate   = "undo_create"
	DecisionPrune        = "prune"
	DecisionQuota        = "quota"
	DecisionVerifyFail   = "verify_fail"
	DecisionOK           = "ok"
	DecisionBroken       = "broken"
	DecisionInterrupted  = "mark_interrupted"
	DecisionUnknown      = "mark_unknown"
	DecisionRestoreCreat = "restore_create"
)

// Record is one audit row. ID is assigned by SQLite and deliberately not part
// of EntryHash; ordering is enforced by ID plus PrevHash.
type Record struct {
	ID        int64  `json:"id"`
	SessionID string `json:"sessionId"`
	TurnID    int64  `json:"turnId"`
	TraceID   string `json:"traceId"`
	Action    string `json:"action"`
	Decision  string `json:"decision"`
	Reason    string `json:"reason"`
	Detail    string `json:"detail"` // canonical JSON object
	CreatedAt string `json:"createdAt"`
	PrevHash  string `json:"prevHash"`
	EntryHash string `json:"entryHash"`
}

// NormalizeDetail validates detail as I-JSON and returns its RFC 8785
// canonical form. An empty detail is normalized to {}.
func NormalizeDetail(detail string) (string, error) {
	if detail == "" {
		return "{}", nil
	}
	canonical, err := jcs.Canonicalize([]byte(detail))
	if err != nil {
		return "", fmt.Errorf("audit: detail: %w", err)
	}
	trimmed := bytes.TrimSpace(canonical)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return "", errors.New("audit: detail must be a JSON object")
	}
	return string(trimmed), nil
}

type hashPayload struct {
	Version   int             `json:"v"`
	Prev      string          `json:"prev"`
	SessionID string          `json:"sessionId"`
	TurnID    int64           `json:"turnId"`
	TraceID   string          `json:"traceId"`
	Action    string          `json:"action"`
	Decision  string          `json:"decision"`
	Reason    string          `json:"reason"`
	Detail    json.RawMessage `json:"detail"`
	CreatedAt string          `json:"createdAt"`
}

// Hash returns the lowercase hex SHA-256 of the canonical record envelope.
// The caller must set CreatedAt before hashing; PrevHash is passed explicitly
// so the same record can be verified against the chain head.
func Hash(prev string, r Record) (string, error) {
	if r.CreatedAt == "" {
		return "", errors.New("audit: createdAt is required")
	}
	detail, err := NormalizeDetail(r.Detail)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(hashPayload{
		Version:   1,
		Prev:      prev,
		SessionID: r.SessionID,
		TurnID:    r.TurnID,
		TraceID:   r.TraceID,
		Action:    r.Action,
		Decision:  r.Decision,
		Reason:    r.Reason,
		Detail:    json.RawMessage(detail),
		CreatedAt: r.CreatedAt,
	})
	if err != nil {
		return "", fmt.Errorf("audit: marshal envelope: %w", err)
	}
	hash, err := jcs.Hash(raw)
	if err != nil {
		return "", fmt.Errorf("audit: hash envelope: %w", err)
	}
	return hash, nil
}
