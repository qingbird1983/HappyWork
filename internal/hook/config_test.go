package hook

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeHookFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func loadBuiltins(t *testing.T, dir string) (*Runner, Info, error) {
	t.Helper()
	return LoadDir(dir, Builtins(), nil)
}

func TestLoadDirValidAndFingerprint(t *testing.T) {
	dir := t.TempDir()
	writeHookFile(t, dir, "10-scan.yaml", `hooks:
  - name: secret-scan
    enabled: true
    priority: 50
    timeout_ms: 300
    on_error: warn
    params:
      mode: warn
`)
	runner, info, err := loadBuiltins(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	if runner.Empty() {
		t.Fatal("secret-scan not registered")
	}
	if len(info.Enabled) != 1 || info.Enabled[0] != "secret-scan" {
		t.Fatalf("enabled = %v", info.Enabled)
	}
	if len(info.Fingerprint) != 64 {
		t.Fatalf("fingerprint = %q", info.Fingerprint)
	}
	_, info2, err := loadBuiltins(t, dir)
	if err != nil || info2.Fingerprint != info.Fingerprint {
		t.Fatalf("fingerprint not stable: %q %q %v", info.Fingerprint, info2.Fingerprint, err)
	}
}

func TestLoadDirDisabledHook(t *testing.T) {
	dir := t.TempDir()
	writeHookFile(t, dir, "hooks.yaml", `hooks:
  - name: secret-scan
    enabled: false
    params:
      mode: block
`)
	runner, info, err := loadBuiltins(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !runner.Empty() || len(info.Enabled) != 0 {
		t.Fatalf("disabled hook loaded: %+v", info)
	}
}

func TestLoadDirRejectsInvalid(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"unknown name", "hooks:\n  - name: nope\n", "unknown hook"},
		{"unknown field", "hooks:\n  - name: secret-scan\n    mode: warn\n", "field mode not found"},
		{"bad timeout", "hooks:\n  - name: secret-scan\n    timeout_ms: 0\n", "timeout_ms"},
		{"huge timeout", "hooks:\n  - name: secret-scan\n    timeout_ms: 5001\n", "timeout_ms"},
		{"bad on_error", "hooks:\n  - name: secret-scan\n    on_error: ignore\n", "on_error"},
		{"unknown param", "hooks:\n  - name: secret-scan\n    params:\n      mode: warn\n      extra: 1\n", "unknown param"},
		{"bad mode", "hooks:\n  - name: secret-scan\n    params:\n      mode: loud\n", "mode"},
		{"duplicate yaml key", "hooks:\n  - name: secret-scan\n    name: secret-scan\n", "already defined"},
		{"multiple docs", "hooks:\n  - name: secret-scan\n---\nhooks: []\n", "multiple YAML documents"},
		{"unknown top key", "hooks: []\nother: 1\n", "field other not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeHookFile(t, dir, "hooks.yaml", tc.body)
			runner, _, err := loadBuiltins(t, dir)
			if err == nil {
				t.Fatalf("invalid config accepted")
			}
			if runner != nil {
				t.Fatalf("runner returned with invalid config")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestLoadDirDuplicateAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	writeHookFile(t, dir, "a.yaml", "hooks:\n  - name: secret-scan\n")
	writeHookFile(t, dir, "b.yaml", "hooks:\n  - name: secret-scan\n")
	if _, _, err := loadBuiltins(t, dir); err == nil || !strings.Contains(err.Error(), "duplicate name") {
		t.Fatalf("duplicate across files accepted: %v", err)
	}
}

func TestLoadDirMissingIsEmpty(t *testing.T) {
	runner, info, err := loadBuiltins(t, filepath.Join(t.TempDir(), "absent"))
	if err != nil || runner == nil || !runner.Empty() || info.Fingerprint != "" {
		t.Fatalf("missing dir = %+v %+v %v", runner, info, err)
	}
}

func TestSecretScanWarnAndBlock(t *testing.T) {
	warn := &secretScan{mode: "warn"}
	ev := Event{Point: BeforeToolCall, Args: map[string]any{
		"content": "key AKIAABCDEFGHIJKLMNOP rest",
		"nested":  map[string]any{"token": "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.signature123456"},
	}}
	out, err := warn.Handle(context.Background(), &ev)
	if err != nil {
		t.Fatal(err)
	}
	if out.DenyReason != "" || len(out.Warnings) != 2 {
		t.Fatalf("warn outcome = %+v", out)
	}
	for _, w := range out.Warnings {
		if strings.Contains(w, "AKIAABCDEFGHIJKLMNOP") || strings.Contains(w, "eyJhbGci") {
			t.Fatalf("matched secret leaked: %q", w)
		}
	}

	block := &secretScan{mode: "block"}
	out, _ = block.Handle(context.Background(), &ev)
	if out.DenyReason == "" {
		t.Fatalf("block mode did not deny at before_tool: %+v", out)
	}
	approval := Event{Point: BeforeApproval, Args: ev.Args}
	out, _ = block.Handle(context.Background(), &approval)
	if out.DenyReason != "" || !out.EscalateBlock {
		t.Fatalf("block approval escalation = %+v", out)
	}

	clean := Event{Point: BeforeToolCall, Args: map[string]any{"content": "hello"}}
	out, _ = warn.Handle(context.Background(), &clean)
	if len(out.Warnings) != 0 {
		t.Fatalf("clean args flagged: %+v", out)
	}
}
