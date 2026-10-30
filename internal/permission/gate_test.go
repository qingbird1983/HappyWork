package permission

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeRules struct {
	persistent map[string]Rule
	session    map[string]bool
	touched    []string
	queried    int
}

func (f *fakeRules) MatchRule(_ context.Context, toolRef, action, matchKey string) (Rule, bool, error) {
	f.queried++
	r, ok := f.persistent[toolRef+"|"+action+"|"+matchKey]
	return r, ok, nil
}

func (f *fakeRules) TouchRule(_ context.Context, id string) error {
	f.touched = append(f.touched, id)
	return nil
}

func (f *fakeRules) HasSessionGrant(_ context.Context, sessionID, matchKey string) (bool, error) {
	f.queried++
	return f.session[sessionID+"|"+matchKey], nil
}

func baseRequest() Request {
	return Request{
		SessionID: "s1", Source: "builtin", Trust: "trusted",
		InsideWorkspace: true, Reversible: true, Preset: "default",
		ContentTrust: Trusted, Now: time.Date(2026, 10, 30, 10, 0, 0, 0, time.Local),
	}
}

func checkVerdict(t *testing.T, name string, got, want Verdict) {
	t.Helper()
	if got.Decision != want.Decision || got.Level != want.Level || got.Scope != want.Scope ||
		got.MatchKey != want.MatchKey || got.Auto != want.Auto ||
		got.PersistentAllowed != want.PersistentAllowed || got.GrantedBy != want.GrantedBy ||
		got.QuotaExceeded != want.QuotaExceeded {
		t.Fatalf("%s: got %+v, want %+v", name, got, want)
	}
}

func TestEngineBaseTable(t *testing.T) {
	e := NewEngine(nil, nil)
	cases := []struct {
		name string
		req  Request
		want Verdict
	}{
		{
			name: "read inside",
			req:  func() Request { r := baseRequest(); r.ToolRef = "builtin://read_file"; r.Action = ActionRead; return r }(),
			want: Verdict{Decision: DecisionAllow, Level: LevelInfo, Scope: ScopeNone, Auto: true},
		},
		{
			name: "write inside",
			req: func() Request {
				r := baseRequest()
				r.ToolRef, r.Action = "builtin://write_file", ActionWrite
				r.MatchKey = "builtin://write_file|write|/ws/docs"
				return r
			}(),
			want: Verdict{Decision: DecisionAsk, Level: LevelConfirm, Scope: ScopeSession,
				MatchKey: "builtin://write_file|write|/ws/docs", PersistentAllowed: true},
		},
		{
			name: "outside denies",
			req: func() Request {
				r := baseRequest()
				r.ToolRef, r.Action, r.InsideWorkspace = "builtin://read_file", ActionRead, false
				return r
			}(),
			want: Verdict{Decision: DecisionDeny},
		},
		{
			name: "sensitive denies",
			req: func() Request {
				r := baseRequest()
				r.ToolRef, r.Action, r.Sensitive = "builtin://read_file", ActionRead, true
				return r
			}(),
			want: Verdict{Decision: DecisionDeny},
		},
		{
			name: "unknown tool",
			req:  func() Request { r := baseRequest(); r.Action = ActionRead; return r }(),
			want: Verdict{Decision: DecisionDeny},
		},
		{
			name: "mcp read session",
			req: func() Request {
				r := baseRequest()
				r.ToolRef, r.Source, r.Action = "mcp://demo/read_note", "mcp:demo", ActionRead
				r.Trust = "untrusted"
				return r
			}(),
			want: Verdict{Decision: DecisionAsk, Level: LevelConfirm, Scope: ScopeSession, MatchKey: "mcp://demo/read_note"},
		},
		{
			name: "mcp write once",
			req: func() Request {
				r := baseRequest()
				r.ToolRef, r.Source, r.Action = "mcp://demo/write", "mcp:demo", ActionWrite
				r.Reversible = false
				return r
			}(),
			want: Verdict{Decision: DecisionAsk, Level: LevelBlock, Scope: ScopeOnce, MatchKey: "mcp://demo/write"},
		},
		{
			name: "mcp destructive blocks",
			req: func() Request {
				r := baseRequest()
				r.ToolRef, r.Source, r.Action = "mcp://demo/delete", "mcp:demo", ActionDelete
				r.Destructive, r.Reversible = true, false
				return r
			}(),
			want: Verdict{Decision: DecisionAsk, Level: LevelBlock, Scope: ScopeOnce, MatchKey: "mcp://demo/delete"},
		},
		{
			name: "shell placeholder denies",
			req:  func() Request { r := baseRequest(); r.ToolRef, r.Action = "builtin://shell", ActionExecute; return r }(),
			want: Verdict{Decision: DecisionDeny},
		},
		{
			name: "unknown preset denies",
			req: func() Request {
				r := baseRequest()
				r.ToolRef, r.Action, r.Preset = "builtin://read_file", ActionRead, "nope"
				return r
			}(),
			want: Verdict{Decision: DecisionDeny},
		},
	}
	for _, tc := range cases {
		got, err := e.Evaluate(context.Background(), tc.req)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		checkVerdict(t, tc.name, got, tc.want)
	}
}

func TestEnginePresets(t *testing.T) {
	rules := &fakeRules{
		persistent: map[string]Rule{"builtin://write_file|write|k": {ID: "r1"}},
		session:    map[string]bool{"s1|k": true},
	}
	e := NewEngine(nil, rules)
	req := baseRequest()
	req.ToolRef, req.Action, req.MatchKey = "builtin://write_file", ActionWrite, "k"

	req.Preset = "default"
	got, _ := e.Evaluate(context.Background(), req)
	checkVerdict(t, "default persistent", got, Verdict{Decision: DecisionAllow, Level: LevelInfo, Scope: ScopePersistent,
		MatchKey: "k", Auto: true, GrantedBy: ScopePersistent})

	req.Preset = "cautious"
	got, _ = e.Evaluate(context.Background(), req)
	checkVerdict(t, "cautious write once", got, Verdict{Decision: DecisionAsk, Level: LevelConfirm, Scope: ScopeOnce, MatchKey: "k"})

	req.Preset = "readonly"
	got, _ = e.Evaluate(context.Background(), req)
	checkVerdict(t, "readonly denies write", got, Verdict{Decision: DecisionDeny})

	mcp := baseRequest()
	mcp.ToolRef, mcp.Source, mcp.Action = "mcp://demo/read", "mcp:demo", ActionRead
	mcp.MatchKey = "mcp://demo/read"
	mcp.Preset = "readonly"
	got, _ = e.Evaluate(context.Background(), mcp)
	checkVerdict(t, "readonly denies mcp", got, Verdict{Decision: DecisionDeny})
}

func TestEngineGrantsAndTrust(t *testing.T) {
	rules := &fakeRules{
		persistent: map[string]Rule{"builtin://write_file|write|k": {ID: "r1"}},
		session:    map[string]bool{"s1|k": true},
	}
	e := NewEngine(nil, rules)
	req := baseRequest()
	req.ToolRef, req.Action, req.MatchKey = "builtin://write_file", ActionWrite, "k"

	// Content trust removes session/persistent and forces once.
	req.ContentTrust = Untrusted
	got, _ := e.Evaluate(context.Background(), req)
	checkVerdict(t, "untrusted write", got, Verdict{Decision: DecisionAsk, Level: LevelConfirm, Scope: ScopeOnce, MatchKey: "k"})

	// MCP read session grant applies; untrusted content does not affect reads.
	mcp := baseRequest()
	mcp.ToolRef, mcp.Source, mcp.Action, mcp.MatchKey = "mcp://demo/read", "mcp:demo", ActionRead, "mcp://demo/read"
	rules.session["s1|mcp://demo/read"] = true
	got, _ = e.Evaluate(context.Background(), mcp)
	checkVerdict(t, "mcp session grant", got, Verdict{Decision: DecisionAllow, Level: LevelInfo, Scope: ScopeSession,
		MatchKey: "mcp://demo/read", Auto: true, GrantedBy: ScopeSession})

	// Preset cautious disables session reuse even when a grant exists.
	mcp.Preset = "cautious"
	got, _ = e.Evaluate(context.Background(), mcp)
	checkVerdict(t, "cautious skips grant", got, Verdict{Decision: DecisionAsk, Level: LevelConfirm, Scope: ScopeOnce, MatchKey: "mcp://demo/read"})

	// Unknown external actions count as side effects: untrusted content forces
	// once even though a session grant exists for the same match key.
	unknown := baseRequest()
	unknown.ToolRef, unknown.Source, unknown.Action, unknown.MatchKey = "mcp://demo/tool", "mcp:demo", ActionUnknown, "mcp://demo/tool"
	rules.session["s1|mcp://demo/tool"] = true
	unknown.ContentTrust = Untrusted
	got, _ = e.Evaluate(context.Background(), unknown)
	checkVerdict(t, "untrusted mcp unknown", got, Verdict{Decision: DecisionAsk, Level: LevelConfirm, Scope: ScopeOnce, MatchKey: "mcp://demo/tool"})
}

func TestPresetScopeOnceDisablesPersistent(t *testing.T) {
	presets := BuiltinPresets()
	presets["strict"] = Preset{
		Name: "strict", Description: "once only", PersistentAllowed: true,
		Rules: []PresetRule{{Tool: "builtin://write_file", Decision: DecisionAsk, Scope: ScopeOnce}},
	}
	e := NewEngine(presets, nil)
	req := baseRequest()
	req.ToolRef, req.Action, req.MatchKey = "builtin://write_file", ActionWrite, "k"
	req.Preset = "strict"
	got, err := e.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Scope != ScopeOnce || got.PersistentAllowed {
		t.Fatalf("preset once must not offer persistent: %+v", got)
	}
}

func TestEngineQuota(t *testing.T) {
	e := NewEngine(nil, nil)
	req := baseRequest()
	req.ToolRef, req.Action = "builtin://read_file", ActionRead
	for i := 0; i < 300; i++ {
		got, err := e.Evaluate(context.Background(), req)
		if err != nil || got.Decision != DecisionAllow {
			t.Fatalf("call %d: %+v, %v", i+1, got, err)
		}
	}
	got, _ := e.Evaluate(context.Background(), req)
	checkVerdict(t, "quota exceeded", got, Verdict{Decision: DecisionAsk, Level: LevelConfirm, Scope: ScopeOnce, QuotaExceeded: true})
}

func TestEngineConditions(t *testing.T) {
	req := baseRequest()
	req.ToolRef, req.Action = "builtin://write_file", ActionWrite
	req.Root, req.Path = "/ws", "/ws/docs/a.txt"
	req.MatchKey = "k"
	cond := Condition{Kind: ConditionPathPrefix, Value: "docs"}
	if !cond.Satisfied(req) {
		t.Fatal("path prefix should match")
	}
	req.Path = "/ws/other/a.txt"
	if cond.Satisfied(req) {
		t.Fatal("path prefix should not match")
	}
	req.Path = "/ws/../etc/passwd"
	if cond.Satisfied(req) {
		t.Fatal("escaping path should not match")
	}
	window := Condition{Kind: ConditionTimeWindow, Value: "09:00-18:00"}
	if !window.Satisfied(req) {
		t.Fatal("time window should match 10:00")
	}
	req.Now = time.Date(2026, 10, 30, 20, 0, 0, 0, time.Local)
	if window.Satisfied(req) {
		t.Fatal("time window should not match 20:00")
	}
	overnight := Condition{Kind: ConditionTimeWindow, Value: "22:00-06:00"}
	req.Now = time.Date(2026, 10, 30, 23, 0, 0, 0, time.Local)
	if !overnight.Satisfied(req) {
		t.Fatal("overnight window should match 23:00")
	}
}

func TestLoadPresets(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("docs.yaml", `
name: docs-only
description: docs only
persistent_allowed: false
rules:
  - tool: builtin://write_file
    decision: ask
    scope: once
    conditions:
      - kind: path_prefix
        value: docs
      - kind: time_window
        value: "09:00-18:00"
`)
	write("widen.yaml", `
name: widen
rules:
  - tool: builtin://write_file
    decision: allow
`)
	write("unknown.yaml", `
name: bad-field
rules:
  - tool: builtin://write_file
    decision: ask
    nope: true
`)
	write("dup.yaml", `
name: readonly
rules:
  - tool: builtin://write_file
    decision: deny
`)
	set, errs := LoadPresets(dir)
	if _, ok := set["docs-only"]; !ok {
		t.Fatalf("valid preset missing: %+v", errs)
	}
	if len(errs) != 3 {
		t.Fatalf("errors = %+v, want 3", errs)
	}
	if _, ok := set["widen"]; ok {
		t.Fatal("widening preset must be rejected")
	}
	if set["readonly"].PersistentAllowed {
		t.Fatal("builtin readonly must not allow persistent")
	}
	names := NewEngine(set, nil).PresetNames()
	if strings.Join(names, ",") != "cautious,default,docs-only,readonly" {
		t.Fatalf("preset names = %v", names)
	}
}

func TestWriteMatchKeyNormalizes(t *testing.T) {
	key := WriteMatchKey(filepath.Join("ws", "docs") + string(filepath.Separator))
	if !strings.HasPrefix(key, "builtin://write_file|write|") {
		t.Fatalf("key = %q", key)
	}
	if filepath.Separator == '\\' && key != strings.ToLower(key) {
		t.Fatalf("windows key not folded: %q", key)
	}
}
