package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/happywork/happywork/internal/workspace"
)

// MaxListEntries caps one directory listing (design.md §6.2).
const MaxListEntries = 1000

const listDirParams = `{
  "type": "object",
  "properties": {
    "path": { "type": "string", "description": "Directory path relative to the workspace root (default .)" }
  }
}`

// DirEntry is one listed entry.
type DirEntry struct {
	Name string `json:"name"`
	Type string `json:"type"` // file | dir | link
	Size int64  `json:"size"`
}

// ListResult is the JSON payload for a directory listing.
type ListResult struct {
	Path      string     `json:"path"`
	Entries   []DirEntry `json:"entries"`
	Count     int        `json:"count"`
	Truncated bool       `json:"truncated"`
}

// ListDir lists one directory inside the workspace. It does not recurse and
// does not follow symlinks; sensitive entries are filtered out entirely (no
// name/size leakage).
type ListDir struct {
	Root workspace.Root
}

// NewListDir builds the tool bound to a canonical workspace root.
func NewListDir(root workspace.Root) *ListDir { return &ListDir{Root: root} }

// Meta implements Tool.
func (t *ListDir) Meta() Meta {
	return Meta{
		Name:        "list_dir",
		Ref:         "builtin://list_dir",
		Description: "List entries of a directory inside the workspace (one level, no symlink following, sensitive entries hidden).",
		Parameters:  json.RawMessage(listDirParams),
		Action:      "read",
		Source:      "builtin",
		Trust:       "trusted",
		Reversible:  true,
		RetrySafe:   true,
	}
}

type listArgs struct {
	Path string `json:"path"`
}

// Exec implements Tool.
func (t *ListDir) Exec(_ context.Context, args json.RawMessage) (any, error) {
	var a listArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("list_dir: bad arguments: %w", err)
	}
	if a.Path == "" {
		a.Path = "."
	}
	dir, err := t.Root.Resolve(a.Path)
	if err != nil {
		return nil, fmt.Errorf("list_dir: %w", err)
	}
	rel, err := t.Root.Rel(dir)
	if err != nil {
		return nil, fmt.Errorf("list_dir: %w", err)
	}
	if workspace.Sensitive(rel) {
		return nil, fmt.Errorf("list_dir: %q: %w", rel, workspace.ErrSensitivePath)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("list_dir: stat %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("list_dir: %s is not a directory", dir)
	}
	raw, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list_dir: read %s: %w", dir, err)
	}
	sort.Slice(raw, func(i, j int) bool { return raw[i].Name() < raw[j].Name() })

	entries := make([]DirEntry, 0, min(len(raw), MaxListEntries))
	truncated := false
	for _, e := range raw {
		entryRel := filepath.Join(rel, e.Name())
		if workspace.Sensitive(entryRel) {
			continue
		}
		if len(entries) >= MaxListEntries {
			truncated = true
			break
		}
		entry := DirEntry{Name: e.Name(), Type: "file"}
		switch {
		case e.Type()&os.ModeSymlink != 0:
			entry.Type = "link"
		case e.IsDir():
			entry.Type = "dir"
		default:
			if fi, err := e.Info(); err == nil {
				entry.Size = fi.Size()
			}
		}
		entries = append(entries, entry)
	}
	return ListResult{Path: dir, Entries: entries, Count: len(entries), Truncated: truncated}, nil
}
