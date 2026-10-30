package tool

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/happywork/happywork/internal/workspace"
)

func newTool(t *testing.T) (*ReadFile, string) {
	t.Helper()
	root, err := workspace.NewRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return NewReadFile(root), root.String()
}

func TestReadFileInside(t *testing.T) {
	rt, root := newTool(t)
	file := filepath.Join(root, "hello.txt")
	if err := os.WriteFile(file, []byte("你好，工作区"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := rt.Exec(context.Background(), json.RawMessage(`{"path":"hello.txt"}`))
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	res, ok := out.(ReadResult)
	if !ok {
		t.Fatalf("Exec result type %T", out)
	}
	if res.Content != "你好，工作区" || res.Truncated || res.SizeBytes == 0 {
		t.Fatalf("unexpected result %+v", res)
	}
	if res.NextOffset != res.SizeBytes {
		t.Fatalf("next offset = %d, size = %d", res.NextOffset, res.SizeBytes)
	}
}

func TestReadFileOutsideDenied(t *testing.T) {
	rt, _ := newTool(t)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"path": outside})
	_, err := rt.Exec(context.Background(), args)
	if !errors.Is(err, workspace.ErrOutsideWorkspace) {
		t.Fatalf("Exec outside err = %v, want ErrOutsideWorkspace", err)
	}
}

func TestReadFileMissingAndDir(t *testing.T) {
	rt, _ := newTool(t)
	if _, err := rt.Exec(context.Background(), json.RawMessage(`{"path":"nope.txt"}`)); err == nil {
		t.Fatal("missing file should error")
	}
	if _, err := rt.Exec(context.Background(), json.RawMessage(`{"path":"."}`)); err == nil {
		t.Fatal("directory should error")
	}
}

func TestReadFileBadArgs(t *testing.T) {
	rt, _ := newTool(t)
	if _, err := rt.Exec(context.Background(), json.RawMessage(`not json`)); err == nil {
		t.Fatal("bad json should error")
	}
	if _, err := rt.Exec(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("missing path should error")
	}
	if _, err := rt.Exec(context.Background(), json.RawMessage(`{"path":"x","offset":-1}`)); err == nil {
		t.Fatal("negative offset should error")
	}
}

func TestReadFilePaging(t *testing.T) {
	rt, root := newTool(t)
	file := filepath.Join(root, "big.txt")
	if err := os.WriteFile(file, []byte("abcdefghij"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := rt.Exec(context.Background(), json.RawMessage(`{"path":"big.txt","max_bytes":4}`))
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	res := out.(ReadResult)
	if res.Content != "abcd" || !res.Truncated || res.NextOffset != 4 || res.SizeBytes != 10 {
		t.Fatalf("unexpected first page %+v", res)
	}
	out, err = rt.Exec(context.Background(), json.RawMessage(`{"path":"big.txt","max_bytes":4,"offset":4}`))
	if err != nil {
		t.Fatalf("Exec page 2: %v", err)
	}
	res = out.(ReadResult)
	if res.Content != "efgh" || !res.Truncated || res.NextOffset != 8 {
		t.Fatalf("unexpected second page %+v", res)
	}
	out, err = rt.Exec(context.Background(), json.RawMessage(`{"path":"big.txt","offset":8,"max_bytes":4}`))
	if err != nil {
		t.Fatalf("Exec page 3: %v", err)
	}
	res = out.(ReadResult)
	if res.Content != "ij" || res.Truncated || res.NextOffset != 10 {
		t.Fatalf("unexpected third page %+v", res)
	}
	// Offset beyond EOF yields an empty, non-truncated page at EOF.
	out, err = rt.Exec(context.Background(), json.RawMessage(`{"path":"big.txt","offset":99}`))
	if err != nil {
		t.Fatalf("Exec beyond EOF: %v", err)
	}
	res = out.(ReadResult)
	if res.Content != "" || res.Truncated || res.Offset != 10 || res.NextOffset != 10 {
		t.Fatalf("unexpected EOF page %+v", res)
	}
}

func TestReadFileDefaultLimit(t *testing.T) {
	rt, root := newTool(t)
	file := filepath.Join(root, "mid.txt")
	content := strings.Repeat("x", DefaultReadFileLimit+10)
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := rt.Exec(context.Background(), json.RawMessage(`{"path":"mid.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	res := out.(ReadResult)
	if len(res.Content) != DefaultReadFileLimit || !res.Truncated || res.NextOffset != DefaultReadFileLimit {
		t.Fatalf("default limit page = %d bytes, truncated=%v, next=%d", len(res.Content), res.Truncated, res.NextOffset)
	}
}

func TestReadFileSensitiveDenied(t *testing.T) {
	rt, root := newTool(t)
	for name, body := range map[string]string{
		".env":         "SECRET=1",
		"server.pem":   "-----BEGIN",
		"sub/.git/x":   "x",
		"id_ed25519":   "key",
		"ok.example":   "fine",
		".env.example": "PUBLIC=1",
	} {
		file := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{".env", "server.pem", "sub/.git/x", "id_ed25519"} {
		args, _ := json.Marshal(map[string]string{"path": name})
		if _, err := rt.Exec(context.Background(), args); !errors.Is(err, workspace.ErrSensitivePath) {
			t.Errorf("%s: err = %v, want ErrSensitivePath", name, err)
		}
	}
	if _, err := rt.Exec(context.Background(), json.RawMessage(`{"path":".env.example"}`)); err != nil {
		t.Fatalf("example env should be readable: %v", err)
	}
}
