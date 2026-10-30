// Package agent implements the session manager and the deterministic turn
// loop (architecture/02). Phase 1 runs the state-machine subset:
//
//	idle → thinking → (running_tool ⇄ thinking)* → completed | error | cancelled
//
// awaiting_approval / paused arrive with the permission engine (Phase 3);
// interrupted is only assigned at startup for crashed sessions.
package agent

// Session states (Phase 1 subset of the architecture/02 state machine).
const (
	StateIdle        = "idle"
	StateThinking    = "thinking"
	StateRunningTool = "running_tool"
	StateCompleted   = "completed"
	StateError       = "error"
	StateCancelled   = "cancelled"
	StateInterrupted = "interrupted"
)

// terminal reports whether a session state ends a run.
func terminal(state string) bool {
	switch state {
	case StateCompleted, StateError, StateCancelled, StateInterrupted:
		return true
	}
	return false
}

// Event names emitted to the frontend and mirrored into the events table.
const (
	EventState = "session:state"
	EventDelta = "session:delta"
	EventTool  = "session:tool"
	EventDone  = "session:done"
)

// MaxTurns caps tool rounds per user message (architecture/02 Budget).
const MaxTurns = 50

// Event payloads (JSON over the wire).
type (
	StatePayload struct {
		SessionID string `json:"sessionID"`
		State     string `json:"state"`
	}
	DeltaPayload struct {
		SessionID string `json:"sessionID"`
		Text      string `json:"text"`
	}
	ToolPayload struct {
		SessionID string `json:"sessionID"`
		Name      string `json:"name"`
		Status    string `json:"status"` // running | ok | failed
		Detail    string `json:"detail,omitempty"`
	}
	DonePayload struct {
		SessionID  string `json:"sessionID"`
		FinalState string `json:"finalState"`
	}
)

// systemPrompt is the Phase 1 system message. The mock provider mostly
// ignores it, but the history assembly is already provider-shaped.
const systemPrompt = `你是 HappyWork，一个本地优先的桌面 Agent（mock 阶段）。
你可以使用工具 read_file 读取工作区内的文本文件。工作区之外的路径会被拒绝。
回答使用用户的语言。`
