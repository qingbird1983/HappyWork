package permission

import (
	"fmt"
	"time"
)

// Row is one entry of the tool mapping table (the single source of truth).
type Row struct {
	Ref        string
	Action     string
	Decision   string
	Scope      string
	Level      string
	Persistent bool
	Reversible bool
	RetrySafe  bool
	Conditions []Condition
	Quota      *Quota
	Reason     string
}

var builtinRows = map[string]Row{
	"builtin://read_file": {
		Ref: "builtin://read_file", Action: ActionRead, Decision: DecisionAllow,
		Scope: ScopeNone, Level: LevelInfo, Reversible: true, RetrySafe: true,
		Quota:  &Quota{Limit: 300, Window: time.Minute, OnExceed: DecisionAsk},
		Reason: "workspace read",
	},
	"builtin://list_dir": {
		Ref: "builtin://list_dir", Action: ActionRead, Decision: DecisionAllow,
		Scope: ScopeNone, Level: LevelInfo, Reversible: true, RetrySafe: true,
		Quota:  &Quota{Limit: 300, Window: time.Minute, OnExceed: DecisionAsk},
		Reason: "workspace read",
	},
	"builtin://write_file": {
		Ref: "builtin://write_file", Action: ActionWrite, Decision: DecisionAsk,
		Scope: ScopeSession, Level: LevelConfirm, Persistent: true, Reversible: true, RetrySafe: true,
		Quota:  &Quota{Limit: 30, Window: time.Minute, OnExceed: DecisionAsk},
		Reason: "workspace write",
	},
	// Placeholder deny rows keep the default posture explicit for actions that
	// are not shipped as executable tools in Phase 3.
	"builtin://delete_file": {
		Ref: "builtin://delete_file", Action: ActionDelete, Decision: DecisionDeny,
		Level: "", Reversible: false, Reason: "action not enabled in phase 3",
	},
	"builtin://shell": {
		Ref: "builtin://shell", Action: ActionExecute, Decision: DecisionDeny,
		Level: "", Reversible: false, Reason: "action not enabled in phase 3",
	},
	"builtin://http_get": {
		Ref: "builtin://http_get", Action: ActionNetwork, Decision: DecisionDeny,
		Level: "", Reversible: false, Reason: "action not enabled in phase 3",
	},
}

// MustCheckTool asserts at startup that a registered tool has a mapping row
// whose action and reversibility agree. A mismatch is a programming error.
func MustCheckTool(ref, action string, reversible bool) {
	row, ok := builtinRows[ref]
	if !ok {
		panic(fmt.Sprintf("permission: tool %q has no mapping row", ref))
	}
	if row.Action != action {
		panic(fmt.Sprintf("permission: tool %q action %q does not match row %q", ref, action, row.Action))
	}
	if row.Reversible != reversible {
		panic(fmt.Sprintf("permission: tool %q reversibility %v does not match row %v", ref, reversible, row.Reversible))
	}
}

// rowFor selects the base row. MCP tools are synthesized from their declared
// action and destructive flag; local tools must be present in the table.
func rowFor(req Request) (Row, bool) {
	if IsMCP(req.Source) {
		row := Row{
			Ref: req.ToolRef, Action: req.Action, Decision: DecisionAsk,
			Scope: ScopeOnce, Level: LevelConfirm, Persistent: false,
			Reversible: req.Reversible, Reason: "external tool",
		}
		if req.Action == ActionRead && !req.Destructive {
			row.Scope = ScopeSession
			row.Level = LevelConfirm
			row.Quota = &Quota{Limit: 60, Window: time.Minute, OnExceed: DecisionAsk}
			row.Reason = "external read tool"
		}
		if req.Destructive {
			row.Level = LevelBlock
			row.Reason = "destructive external tool"
		}
		return row, true
	}
	row, ok := builtinRows[req.ToolRef]
	return row, ok
}

func sideEffect(action string) bool {
	switch action {
	case ActionWrite, ActionDelete, ActionExecute, ActionNetwork, ActionUnknown:
		// Unknown covers external tools whose action could not be classified:
		// treat them as side-effecting rather than assuming a harmless read.
		return true
	}
	return false
}

func decisionRank(d string) int {
	switch d {
	case DecisionAllow:
		return 0
	case DecisionAsk:
		return 1
	case DecisionDeny:
		return 2
	}
	return 3
}

func scopeRank(s string) int {
	switch s {
	case ScopeNone:
		return 0
	case ScopeOnce:
		return 1
	case ScopeSession:
		return 2
	case ScopePersistent:
		return 3
	}
	return 0
}

func levelRank(l string) int {
	switch l {
	case LevelInfo:
		return 0
	case LevelConfirm:
		return 1
	case LevelBlock:
		return 2
	}
	return 0
}
