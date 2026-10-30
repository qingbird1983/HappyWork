// Package storage defines the persistence contract for sessions, messages,
// tool calls, events and audit rows, plus the SQLite implementation.
// The driver (modernc.org/sqlite, ADR-0010) is only referenced in sqlite.go.
package storage

import (
	"context"
	"time"
)

// Session is one conversation bound to a fixed workspace root.
type Session struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	WorkspaceRoot string `json:"workspaceRoot"`
	State         string `json:"state"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}

// Message roles.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
	RoleSystem    = "system"
)

// Message is one transcript entry. role=tool carries toolCallID/toolName;
// role=assistant may carry toolCallsJSON for future providers.
type Message struct {
	ID            string `json:"id"`
	SessionID     string `json:"sessionId"`
	Seq           int64  `json:"seq"`
	Role          string `json:"role"`
	Content       string `json:"content"`
	ToolCallID    string `json:"toolCallId,omitempty"`
	ToolName      string `json:"toolName,omitempty"`
	ToolCallsJSON string `json:"toolCallsJson,omitempty"`
	CreatedAt     string `json:"createdAt"`
}

// ToolCallRecord tracks one tool invocation and its outcome.
type ToolCallRecord struct {
	ID         string
	SessionID  string
	Name       string
	ArgsJSON   string
	ResultJSON string
	Status     string
	CreatedAt  string
}

// AuditEntry is one immutable audit row (A5 boundary denials; richer use in Phase 4).
type AuditEntry struct {
	ID        int64  `json:"id"`
	SessionID string `json:"sessionId"`
	Action    string `json:"action"`
	Decision  string `json:"decision"`
	Reason    string `json:"reason"`
	CreatedAt string `json:"createdAt"`
}

// Store is the persistence surface used by the agent loop.
type Store interface {
	CreateSession(ctx context.Context, s *Session) error
	ListSessions(ctx context.Context) ([]Session, error)
	GetSession(ctx context.Context, id string) (Session, error)
	UpdateSessionState(ctx context.Context, id, state string) error
	UpdateSessionTitle(ctx context.Context, id, title string) error

	AppendMessage(ctx context.Context, m *Message) error
	Transcript(ctx context.Context, sessionID string) ([]Message, error)

	AppendToolCall(ctx context.Context, tc *ToolCallRecord) error
	UpdateToolCallResult(ctx context.Context, id, resultJSON, status string) error

	AppendEvent(ctx context.Context, sessionID, typ, payloadJSON string) error
	AppendAudit(ctx context.Context, sessionID, action, decision, reason string) error
	ListAudit(ctx context.Context, sessionID string) ([]AuditEntry, error)

	KVGet(ctx context.Context, key string) (string, error)
	KVSet(ctx context.Context, key, value string) error

	MarkInterruptedNonTerminal(ctx context.Context) (int64, error)
	Close() error
}

// Now returns the canonical timestamp format used in all tables.
func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
