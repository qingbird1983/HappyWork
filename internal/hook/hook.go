// Package hook implements the in-process, compiled-in hook framework
// (design/phase-3/hooks.md, ADR-0013). Hooks observe and, at before_tool_call
// only, propose argument patches; the runner decides what is accepted.
package hook

import (
	"context"
	"time"
)

// Point names one lifecycle position. Only compiled-in code registers hooks.
type Point string

// The five points (hooks.md §2).
const (
	BeforeModelCall Point = "before_model_call"
	AfterModelCall  Point = "after_model_call"
	BeforeToolCall  Point = "before_tool_call"
	AfterToolCall   Point = "after_tool_call"
	BeforeApproval  Point = "before_approval"
)

// DefaultTimeout and MaxTimeout bound one hook invocation.
const (
	DefaultTimeout = 500 * time.Millisecond
	MaxTimeout     = 5 * time.Second
)

// Failure policies (hooks.md §4).
const (
	OnErrorWarn       = "warn"
	OnErrorDeny       = "deny"
	OnErrorFailClosed = "fail_closed"
)

// Audit kinds written through AuditFunc.
const (
	KindPatch      = "hook_patch"
	KindDeny       = "hook_deny"
	KindError      = "hook_error"
	KindFailClosed = "hook_fail_closed"
)

// Event is the read-only view handed to a hook. Args is mutable only at
// before_tool_call, and even then only through Patch outcomes; direct map
// mutation is ignored. Warnings accumulate across hooks.
type Event struct {
	Point     Point
	SessionID string
	TurnID    int64
	ToolRef   string
	ToolName  string
	Action    string
	Trust     string
	Args      map[string]any
	Warnings  []string
}

// Patch asks the runner to replace one top-level argument field.
type Patch struct {
	Field string
	Value any
}

// Outcome is a hook's suggestion. The runner validates patches, caps warnings
// and treats DenyReason as final for the current call.
type Outcome struct {
	Patches    []Patch
	Warnings   []string
	DenyReason string
	// EscalateBlock is honoured only at before_approval: the dialog is raised
	// to block level, which requires the typed confirmation phrase.
	EscalateBlock bool
}

// Hook is one compiled-in extension. Priority orders execution (lower first,
// ties by Name).
type Hook interface {
	Name() string
	Points() []Point
	Priority() int
	Handle(ctx context.Context, ev *Event) (Outcome, error)
}

// Entry binds a hook to its effective configuration.
type Entry struct {
	Hook     Hook
	Priority int
	Timeout  time.Duration
	OnError  string
}

// AuditFunc persists one hook action. reason never contains argument or result
// values, only names, fields and hashes.
type AuditFunc func(ev Event, hookName, kind, reason string)
