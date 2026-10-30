// Package permission holds the Phase 2 static decision table. It is a pure
// function over already-normalized data: callers resolve paths and metadata
// before asking; this package never imports tool or workspace.
//
// Phase 3 replaces the body with the full mapping-table engine (architecture/03);
// the Decision/Request/Verdict shape is kept stable so the actor does not change.
package permission

import (
	"path/filepath"
	"strings"
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

// Tool actions.
const (
	ActionRead    = "read"
	ActionWrite   = "write"
	ActionExecute = "execute"
	ActionDelete  = "delete"
	ActionNetwork = "network"
	ActionUnknown = "unknown"
)

// Request is the normalized input to the gate.
type Request struct {
	ToolRef         string
	Name            string
	Source          string // builtin | mcp:<server>
	Trust           string // trusted | untrusted
	Action          string
	InsideWorkspace bool
	Sensitive       bool // hard-deny table hit (workspace.Sensitive)
	Destructive     bool // MCP config declared destructive
	MatchKey        string
}

// Verdict is the gate's decision.
type Verdict struct {
	Decision string
	Level    string
	MatchKey string
	Reason   string
}

// Gate is stateless; callers may hold one per app.
type Gate struct{}

// Evaluate applies the Phase 2 static table (design.md §5.1):
//
//	sensitive / outside workspace / unknown builtin → deny
//	MCP (any)                                        → ask (confirm or block)
//	builtin read                                     → allow (info)
//	builtin write                                    → ask (confirm)
//	everything else                                  → deny
func (Gate) Evaluate(req Request) Verdict {
	if req.Sensitive {
		return Verdict{Decision: DecisionDeny, Reason: "sensitive path"}
	}
	if !req.InsideWorkspace {
		return Verdict{Decision: DecisionDeny, Reason: "outside workspace"}
	}
	if req.ToolRef == "" {
		return Verdict{Decision: DecisionDeny, Reason: "unknown tool"}
	}
	if IsMCP(req.Source) {
		level := LevelConfirm
		if req.Destructive {
			level = LevelBlock
		}
		return Verdict{
			Decision: DecisionAsk,
			Level:    level,
			MatchKey: MCPMatchKey(req.ToolRef),
			Reason:   "external tool",
		}
	}
	switch req.Action {
	case ActionRead:
		return Verdict{Decision: DecisionAllow, Level: LevelInfo, Reason: "workspace read"}
	case ActionWrite:
		return Verdict{
			Decision: DecisionAsk,
			Level:    LevelConfirm,
			MatchKey: req.MatchKey,
			Reason:   "workspace write",
		}
	default:
		return Verdict{Decision: DecisionDeny, Reason: "action not enabled in phase 2"}
	}
}

// IsMCP reports whether the tool source denotes an MCP server.
func IsMCP(source string) bool { return strings.HasPrefix(source, "mcp:") }

// MCPMatchKey is the approval scope key for an external tool. There is no
// session scope for MCP; the key still identifies the request for auditing.
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
