// Package permission is the single gate for every tool call (architecture/03).
// It is a pure decision engine over already-normalized data: callers resolve
// paths, classify trust and supply the preset before asking. The engine never
// imports tool, workspace or storage.
package permission

import (
	"context"
	"path/filepath"
	"strings"
	"time"
)

// Decisions.
const (
	DecisionAllow = "allow"
	DecisionAsk   = "ask"
	DecisionDeny  = "deny"
)

// Dialog levels (research/03): info is event-only, confirm shows the dialog,
// block additionally requires a typed confirmation phrase.
const (
	LevelInfo    = "info"
	LevelConfirm = "confirm"
	LevelBlock   = "block"
)

// Approval scopes. Narrower is stricter.
const (
	ScopeNone       = "none"
	ScopeOnce       = "once"
	ScopeSession    = "session"
	ScopePersistent = "persistent"
)

// Content trust levels (see ADR-0014).
const (
	Trusted   = "trusted"
	Untrusted = "untrusted"
)

// Tool actions.
const (
	ActionRead    = "read"
	ActionWrite   = "write"
	ActionExecute = "execute"
	ActionDelete  = "delete"
	ActionNetwork = "network"
	ActionUnknown = "unknown"
)

// Request is the normalized input to the engine. Path and Root are set for
// filesystem tools and feed path_prefix conditions; Domain is reserved for
// network tools (Phase 4+).
type Request struct {
	SessionID       string
	ToolRef         string
	Name            string
	Source          string // builtin | mcp:<server>
	Trust           string // trusted | untrusted
	Action          string
	InsideWorkspace bool
	Sensitive       bool // hard-deny table hit (workspace.Sensitive)
	Destructive     bool // MCP config declared destructive
	Reversible      bool
	MatchKey        string
	Path            string // canonical absolute path for filesystem tools
	Root            string // canonical workspace root
	Domain          string // canonical domain for network tools
	Preset          string
	ContentTrust    string // trusted | untrusted (ADR-0014)
	Now             time.Time
}

// Verdict is the engine's unique decision.
type Verdict struct {
	Decision          string
	Level             string
	Scope             string
	MatchKey          string
	Reason            string
	Auto              bool // allow without a new human dialog
	QuotaExceeded     bool
	PersistentAllowed bool
	GrantedBy         string // "" | session | persistent
}

// Rule is one persistent approval. It only ever grants ask→allow.
type Rule struct {
	ID       string
	ToolRef  string
	Action   string
	MatchKey string
}

// RuleStore resolves persistent rules and session grants. Implementations live
// above storage (agent adapter) so this package stays storage-free.
type RuleStore interface {
	MatchRule(ctx context.Context, toolRef, action, matchKey string) (Rule, bool, error)
	TouchRule(ctx context.Context, id string) error
	HasSessionGrant(ctx context.Context, sessionID, matchKey string) (bool, error)
}

// IsMCP reports whether the tool source denotes an MCP server.
func IsMCP(source string) bool { return strings.HasPrefix(source, "mcp:") }

// MCPMatchKey is the approval scope key for an external tool. Session scope is
// only offered for read-only MCP tools; the key still identifies the request.
func MCPMatchKey(toolRef string) string { return toolRef }

// WriteMatchKey scopes a write approval to the canonical parent directory.
// Windows paths fold case because the filesystem does.
func WriteMatchKey(parentDir string) string {
	parentDir = strings.TrimRight(parentDir, `/\`)
	if isWindows() {
		parentDir = strings.ToLower(parentDir)
	}
	return "builtin://write_file|write|" + parentDir
}

func isWindows() bool { return filepath.Separator == '\\' }
