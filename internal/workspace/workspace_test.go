package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newTestRoot(t *testing.T) Root {
	t.Helper()
	root, err := NewRoot(t.TempDir())
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	return root
}

func TestResolveInside(t *testing.T) {
	root := newTestRoot(t)
	inside := filepath.Join(root.String(), "sub", "file.txt")
	if err := os.MkdirAll(filepath.Dir(inside), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		input string
	}{
		{"relative", "sub/file.txt"},
		{"relative dotted", filepath.Join(".", "sub", ".", "file.txt")},
		{"absolute", inside},
	}
	if runtime.GOOS == "windows" {
		upper := filepath.Join(strings.ToUpper(root.String()), "SUB", "file.txt")
		cases = append(cases, struct{ name, input string }{"case-insensitive", upper})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := root.Resolve(tc.input)
			if err != nil {
				t.Fatalf("Resolve(%q): %v", tc.input, err)
			}
			if !filepath.IsAbs(got) {
				t.Fatalf("Resolve(%q) = %q, want absolute", tc.input, got)
			}
			if !root.contains(got) {
				t.Fatalf("Resolve(%q) = %q escapes root %q", tc.input, got, root.String())
			}
		})
	}
}

func TestResolveOutside(t *testing.T) {
	root := newTestRoot(t)
	outside := t.TempDir() // a different directory entirely
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		input string
	}{
		{"dotdot relative", filepath.Join("..", filepath.Base(outside), "secret.txt")},
		{"absolute outside", filepath.Join(outside, "secret.txt")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := root.Resolve(tc.input)
			if !errors.Is(err, ErrOutsideWorkspace) {
				t.Fatalf("Resolve(%q) err = %v, want ErrOutsideWorkspace", tc.input, err)
			}
		})
	}
}

func TestResolveInvalidForms(t *testing.T) {
	root := newTestRoot(t)
	cases := []struct {
		name  string
		input string
	}{
		{"unc", `\\server\share\file.txt`},
		{"unc forward", "//server/share/file.txt"},
		{"short name", filepath.Join(root.String(), "LONGFI~1.TXT")},
		{"short name mid", filepath.Join(root.String(), "ABC123~2", "x.txt")},
	}
	if runtime.GOOS == "windows" {
		cases = append(cases, struct{ name, input string }{"drive relative", "C:foo.txt"})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := root.Resolve(tc.input)
			if !errors.Is(err, ErrInvalidPath) {
				t.Fatalf("Resolve(%q) err = %v, want ErrInvalidPath", tc.input, err)
			}
		})
	}
}

func TestResolveSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows; junction escape covered by EvalSymlinks re-check")
	}
	root := newTestRoot(t)
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root.String(), "innocent")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := root.Resolve("innocent")
	if !errors.Is(err, ErrOutsideWorkspace) {
		t.Fatalf("Resolve(symlink) err = %v, want ErrOutsideWorkspace", err)
	}
}

func TestResolveMissingFile(t *testing.T) {
	root := newTestRoot(t)
	// Inside the workspace the boundary passes; the missing file error comes
	// from the filesystem layer (tool), not the boundary check.
	got, err := root.Resolve("nope.txt")
	if err != nil {
		t.Fatalf("Resolve(missing inside) = %v, want nil", err)
	}
	if got == "" {
		t.Fatal("empty path")
	}
}

func TestNewRootRejects(t *testing.T) {
	if _, err := NewRoot(""); err == nil {
		t.Fatal(`NewRoot("") should fail`)
	}
	if runtime.GOOS == "windows" {
		// UNC is a dangerous alias only on Windows; on POSIX a leading `\\`
		// is just an odd literal directory name and must stay resolvable.
		if _, err := NewRoot(`\\server\share`); !errors.Is(err, ErrInvalidPath) {
			t.Fatalf("NewRoot(UNC) = %v, want ErrInvalidPath", err)
		}
	}
}
