// Package model defines the provider contract between the agent loop and
// model backends. Concrete providers (mock now, DeepSeek in Phase 2) live in
// subpackages and must stay behind this interface.
package model

import (
	"context"
	"encoding/json"
)

// Message roles as sent to a provider.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// ToolCall is one tool invocation requested by the model.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// Message is one turn element in the provider conversation history.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`   // assistant messages that call tools
	ToolCallID string     `json:"tool_call_id,omitempty"` // tool result messages
}

// ToolSpec describes one callable tool for the provider's tools parameter.
type ToolSpec struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Parameters is the JSON-schema object for the tool's arguments.
	Parameters json.RawMessage `json:"parameters"`
}

// Chunk is one streamed provider event. Exactly one field is meaningful per
// chunk: Delta (text), ToolCall (a complete call; mock providers do not split
// arguments), Done (terminal ok) or Err.
type Chunk struct {
	Delta    string
	ToolCall *ToolCall
	Done     bool
	Err      error
}

// Provider streams one assistant turn. Implementations must close the channel
// in all cases and must honour ctx cancellation.
type Provider interface {
	Stream(ctx context.Context, msgs []Message, tools []ToolSpec) <-chan Chunk
}

// Selector picks the mock script for a user turn. Only used by the mock
// provider; the signature stays here so the agent loop can construct providers
// without importing the mock package.
type Selector func(userText string, turn int) string
