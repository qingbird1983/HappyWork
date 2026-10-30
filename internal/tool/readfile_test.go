package tool

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/happywork/happywork/internal/workspace"
)

func newTool(t *testing.T, limit int64) (*ReadFile, string) {
	t.Helper()
	root, err := workspace.NewRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return NewReadFile(root, limit), root.String()
}

func TestReadFileInside(t *testing.T) {
	rt, root := newTool(t, 0)
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
}

func TestReadFileOutsideDenied(t *testing.T) {
	rt, _ := newTool(t, 0)
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
	rt, root := newTool(t, 0)
	if _, err := rt.Exec(context.Background(), json.RawMessage(`{"path":"nope.txt"}`)); err == nil {
		t.Fatal("missing file should error")
	}
	if _, err := rt.Exec(context.Background(), json.RawMessage(`{"path":"."}`)); err == nil {
		t.Fatal("directory should error")
	}
	_ = root
}

func TestReadFileBadArgs(t *testing.T) {
	rt, _ := newTool(t, 0)
	if _, err := rt.Exec(context.Background(), json.RawMessage(`not json`)); err == nil {
		t.Fatal("bad json should error")
	}
	if _, err := rt.Exec(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("missing path should error")
	}
}

func TestReadFileTruncates(t *testing.T) {
	rt, root := newTool(t, 4)
	file := filepath.Join(root, "big.txt")
	if err := os.WriteFile(file, []byte("abcdefghij"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := rt.Exec(context.Background(), json.RawMessage(`{"path":"big.txt"}`))
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	res := out.(ReadResult)
	if res.Content != "abcd" || !res.Truncated || res.SizeBytes != 10 {
		t.Fatalf("unexpected result %+v", res)
	}
}
