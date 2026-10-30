// Package tool holds the built-in tool registry and tool implementations.
// Phase 1 ships exactly one tool (read_file) with a hardcoded workspace
// boundary check; the full permission engine arrives in Phase 3.
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/happywork/happywork/internal/model"
)

// Meta describes a tool for the registry and the permission mapping table.
// Reversible/RetrySafe follow architecture/04 ToolMeta semantics; Action,
// Source and Trust feed the Phase 2 decision table (design.md §6.2).
type Meta struct {
	Name string
	Ref  string // e.g. builtin://read_file
	// Description and Parameters feed the provider's tools entry.
	Description string
	Parameters  json.RawMessage // JSON schema for the arguments object
	Action      string          // read | write | execute | delete | network | unknown
	Source      string          // builtin | mcp:<server>
	Trust       string          // trusted | untrusted
	Destructive bool            // MCP config marked it destructive (block level)
	Reversible  bool
	RetrySafe   bool
}

// Tool is one executable tool. Exec receives canonical JSON arguments and
// returns a JSON-marshalable result.
type Tool interface {
	Meta() Meta
	Exec(ctx context.Context, args json.RawMessage) (any, error)
}

// Registry is the set of tools available to sessions. Built-in tools are
// static; external tools (MCP) are a live snapshot read on every call, so a
// server restart is reflected without rebuilding sessions.
type Registry struct {
	byName   map[string]Tool
	external func() []Tool
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byName: map[string]Tool{}}
}

// Register adds a tool; duplicate names panic (programming error at startup).
func (r *Registry) Register(t Tool) {
	m := t.Meta()
	if _, dup := r.byName[m.Name]; dup {
		panic(fmt.Sprintf("tool: duplicate registration %q", m.Name))
	}
	r.byName[m.Name] = t
}

// SetExternal installs (or clears, with nil) the external tool snapshot
// provider. It must be safe for concurrent calls.
func (r *Registry) SetExternal(fn func() []Tool) { r.external = fn }

// Get returns the tool registered under name, consulting built-ins first.
func (r *Registry) Get(name string) (Tool, bool) {
	if t, ok := r.byName[name]; ok {
		return t, true
	}
	for _, t := range r.externalTools() {
		if t.Meta().Name == name {
			return t, true
		}
	}
	return nil, false
}

// Specs renders the registry for the provider tools parameter (sorted by name
// for deterministic history assembly).
func (r *Registry) Specs() []model.ToolSpec {
	all := make(map[string]Tool, len(r.byName))
	for n, t := range r.byName {
		all[n] = t
	}
	for _, t := range r.externalTools() {
		m := t.Meta()
		if _, exists := all[m.Name]; !exists {
			all[m.Name] = t
		}
	}
	names := make([]string, 0, len(all))
	for n := range all {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]model.ToolSpec, 0, len(names))
	for _, n := range names {
		m := all[n].Meta()
		out = append(out, model.ToolSpec{
			Name:        m.Name,
			Description: m.Description,
			Parameters:  m.Parameters,
		})
	}
	return out
}

func (r *Registry) externalTools() []Tool {
	if r.external == nil {
		return nil
	}
	return r.external()
}
