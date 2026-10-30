package tool

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/happywork/happywork/internal/rollback"
	"github.com/happywork/happywork/internal/storage"
	"github.com/happywork/happywork/internal/workspace"
)

func newWrite(t *testing.T) (*WriteFile, string, string) {
	t.Helper()
	base := t.TempDir()
	wsDir := filepath.Join(base, "ws")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := workspace.NewRoot(wsDir)
	if err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(filepath.Join(base, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateSession(context.Background(), &storage.Session{ID: "sess1", WorkspaceRoot: root.String()}); err != nil {
		t.Fatal(err)
	}
	backups := filepath.Join(base, "backups")
	writer := rollback.NewWriter(root, backups, "sess1", store, rollback.DefaultLimits())
	return NewWriteFile(root, writer), root.String(), backups
}

func TestWriteFileCreateAndOverwrite(t *testing.T) {
	wt, root, backups := newWrite(t)
	out, err := wt.Exec(context.Background(), json.RawMessage(`{"path":"docs/a.md","content":"你好"}`))
	if err == nil {
		t.Fatalf("missing parent should fail, got %+v", out)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err = wt.Exec(context.Background(), json.RawMessage(`{"path":"docs/a.md","content":"第一版"}`))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	res := out.(WriteResult)
	if !res.Created || res.BackupSaved || res.BytesWritten != len("第一版") || !res.Verified || res.Version != 1 {
		t.Fatalf("create result = %+v", res)
	}
	if err := os.Chmod(filepath.Join(root, "docs", "a.md"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err = wt.Exec(context.Background(), json.RawMessage(`{"path":"docs/a.md","content":"第二版"}`))
	if err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	res = out.(WriteResult)
	if res.Created || !res.BackupSaved || !res.Verified || res.BackupID == "" || res.Version != 2 {
		t.Fatalf("overwrite result = %+v", res)
	}
	body, _ := os.ReadFile(filepath.Join(root, "docs", "a.md"))
	if string(body) != "第二版" {
		t.Fatalf("content = %q", body)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(root, "docs", "a.md"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %o, want 600 (preserved)", info.Mode().Perm())
		}
	}
	versions, err := filepath.Glob(filepath.Join(backups, "sess1", "docs", "a.md", "*.bak"))
	if err != nil || len(versions) != 1 {
		t.Fatalf("backup versions = %v, %v", versions, err)
	}
	backup, err := os.ReadFile(versions[0])
	if err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	if string(backup) != "第一版" {
		t.Fatalf("backup content = %q", backup)
	}
	// No temp files left behind.
	entries, _ := os.ReadDir(filepath.Join(root, "docs"))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".happywork-write-") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func TestWriteFileRejections(t *testing.T) {
	wt, root, _ := newWrite(t)
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		args string
	}{
		{"empty path", `{"path":"","content":"x"}`},
		{"nul", `{"path":"docs/x.txt","content":"a\u0000b"}`},
		{"too large", `{"path":"docs/x.txt","content":"` + strings.Repeat("x", MaxWriteFileBytes+1) + `"}`},
		{"sensitive env", `{"path":".env","content":"x"}`},
		{"sensitive git", `{"path":".git/config","content":"x"}`},
		{"sensitive key", `{"path":"docs/server.pem","content":"x"}`},
		{"bad json", `not json`},
	}
	for _, tc := range cases {
		if _, err := wt.Exec(context.Background(), json.RawMessage(tc.args)); err == nil {
			t.Errorf("%s: expected error", tc.name)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".env")); !os.IsNotExist(err) {
		t.Fatal("sensitive file must not be created")
	}
}

func TestWriteFileRefusesUnbackedOverwrite(t *testing.T) {
	base := t.TempDir()
	rootDir := filepath.Join(base, "ws")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "a.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := workspace.NewRoot(rootDir)
	if err != nil {
		t.Fatal(err)
	}
	wt := NewWriteFile(root, nil)
	_, err = wt.Exec(context.Background(), json.RawMessage(`{"path":"a.txt","content":"new"}`))
	if err == nil || !strings.Contains(err.Error(), "backup writer") {
		t.Fatalf("err = %v, want backup refusal", err)
	}
	body, _ := os.ReadFile(filepath.Join(rootDir, "a.txt"))
	if string(body) != "old" {
		t.Fatal("file changed despite refusal")
	}
}

func TestWriteFileSensitiveError(t *testing.T) {
	wt, root, _ := newWrite(t)
	_ = os.MkdirAll(filepath.Join(root, "sub"), 0o755)
	_, err := wt.Exec(context.Background(), json.RawMessage(`{"path":"sub/.ssh/id_rsa","content":"x"}`))
	if !errors.Is(err, workspace.ErrSensitivePath) {
		t.Fatalf("err = %v, want ErrSensitivePath", err)
	}
}
