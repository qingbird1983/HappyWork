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

// Meta describes a tool for the registry and (later) the permission mapping
// table. Reversible/RetrySafe follow architecture/04 ToolMeta semantics.
type Meta struct {
	Name string
	Ref  string // e.g. builtin://read_file
	// Description and Parameters feed the provider's tools entry.
	Description string
	Parameters  json.RawMessage // JSON schema for the arguments object
	Reversible  bool
	RetrySafe   bool
}

// Tool is one executable tool. Exec receives canonical JSON arguments and
// returns a JSON-marshalable result.
type Tool interface {
	Meta() Meta
	Exec(ctx context.Context, args json.RawMessage) (any, error)
}

// Registry is the set of tools available to sessions.
type Registry struct {
	byName map[string]Tool
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

// Get returns the tool registered under name.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.byName[name]
	return t, ok
}

// Specs renders the registry for the provider tools parameter (sorted by name
// for deterministic history assembly).
func (r *Registry) Specs() []model.ToolSpec {
	names := make([]string, 0, len(r.byName))
	for n := range r.byName {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]model.ToolSpec, 0, len(names))
	for _, n := range names {
		m := r.byName[n].Meta()
		out = append(out, model.ToolSpec{
			Name:        m.Name,
			Description: m.Description,
			Parameters:  m.Parameters,
		})
	}
	return out
}
