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

func newList(t *testing.T) (*ListDir, string) {
	t.Helper()
	root, err := workspace.NewRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return NewListDir(root), root.String()
}

func TestListDirSortsAndFiltersSensitive(t *testing.T) {
	lt, root := newList(t)
	for _, name := range []string{"b.txt", "a.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".env", ".git", ".env.example", "id_rsa"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("secret"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, err := lt.Exec(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	res := out.(ListResult)
	var names []string
	for _, e := range res.Entries {
		names = append(names, e.Name)
		if e.Name == ".env" || e.Name == ".git" || e.Name == "id_rsa" {
			t.Fatalf("sensitive entry leaked: %+v", e)
		}
	}
	want := []string{".env.example", "a.txt", "b.txt", "dir"}
	if len(names) != len(want) {
		t.Fatalf("entries = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("entries = %v, want %v", names, want)
		}
	}
	for _, e := range res.Entries {
		if e.Name == "dir" && e.Type != "dir" {
			t.Fatalf("dir type = %q", e.Type)
		}
		if e.Name == "a.txt" && (e.Type != "file" || e.Size != 1) {
			t.Fatalf("a.txt entry = %+v", e)
		}
	}
}

func TestListDirSensitiveDirDenied(t *testing.T) {
	lt, root := newList(t)
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := lt.Exec(context.Background(), json.RawMessage(`{"path":".git"}`)); !errors.Is(err, workspace.ErrSensitivePath) {
		t.Fatalf("listing .git err = %v, want ErrSensitivePath", err)
	}
	if _, err := lt.Exec(context.Background(), json.RawMessage(`{"path":"nope"}`)); err == nil {
		t.Fatal("missing dir should error")
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := lt.Exec(context.Background(), json.RawMessage(`{"path":"file.txt"}`)); err == nil {
		t.Fatal("file path should error")
	}
}
