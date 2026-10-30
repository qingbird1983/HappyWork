package permission

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestGateGolden(t *testing.T) {
	g := Gate{}
	cases := []struct {
		name string
		req  Request
		want Verdict
	}{
		{
			name: "read inside",
			req:  Request{ToolRef: "builtin://read_file", Source: "builtin", Action: ActionRead, InsideWorkspace: true},
			want: Verdict{Decision: DecisionAllow, Level: LevelInfo, Reason: "workspace read"},
		},
		{
			name: "write inside asks with parent key",
			req: Request{ToolRef: "builtin://write_file", Source: "builtin", Action: ActionWrite,
				InsideWorkspace: true, MatchKey: "builtin://write_file|write|/ws/docs"},
			want: Verdict{Decision: DecisionAsk, Level: LevelConfirm, MatchKey: "builtin://write_file|write|/ws/docs", Reason: "workspace write"},
		},
		{
			name: "outside denies",
			req:  Request{ToolRef: "builtin://read_file", Source: "builtin", Action: ActionRead},
			want: Verdict{Decision: DecisionDeny, Reason: "outside workspace"},
		},
		{
			name: "sensitive denies even with approval-ish data",
			req: Request{ToolRef: "builtin://read_file", Source: "builtin", Action: ActionRead,
				InsideWorkspace: true, Sensitive: true},
			want: Verdict{Decision: DecisionDeny, Reason: "sensitive path"},
		},
		{
			name: "unknown tool denies",
			req:  Request{ToolRef: "", Source: "builtin", Action: ActionRead, InsideWorkspace: true},
			want: Verdict{Decision: DecisionDeny, Reason: "unknown tool"},
		},
		{
			name: "mcp asks once each time",
			req: Request{ToolRef: "mcp://demo/read_note", Source: "mcp:demo", Action: ActionUnknown,
				InsideWorkspace: true, Trust: "untrusted"},
			want: Verdict{Decision: DecisionAsk, Level: LevelConfirm, MatchKey: "mcp://demo/read_note", Reason: "external tool"},
		},
		{
			name: "mcp destructive blocks",
			req: Request{ToolRef: "mcp://demo/delete", Source: "mcp:demo", Action: ActionDelete,
				InsideWorkspace: true, Destructive: true},
			want: Verdict{Decision: DecisionAsk, Level: LevelBlock, MatchKey: "mcp://demo/delete", Reason: "external tool"},
		},
		{
			name: "execute not enabled",
			req:  Request{ToolRef: "builtin://shell", Source: "builtin", Action: ActionExecute, InsideWorkspace: true},
			want: Verdict{Decision: DecisionDeny, Reason: "action not enabled in phase 2"},
		},
	}
	for _, tc := range cases {
		if got := g.Evaluate(tc.req); got != tc.want {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
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
