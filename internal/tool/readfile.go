package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/happywork/happywork/internal/workspace"
)

const (
	// DefaultReadFileLimit is the per-call default (design.md §6.2).
	DefaultReadFileLimit = 256 << 10
	// MaxReadFileLimit is the hard per-call cap.
	MaxReadFileLimit = 2 << 20
)

// readFileParams is the JSON schema advertised to providers.
const readFileParams = `{
  "type": "object",
  "properties": {
    "path": { "type": "string", "description": "File path, absolute or relative to the workspace root" },
    "offset": { "type": "integer", "minimum": 0, "description": "Byte offset to start reading from (default 0)" },
    "max_bytes": { "type": "integer", "minimum": 1, "description": "Maximum bytes to return (default 262144, hard cap 2097152)" }
  },
  "required": ["path"]
}`

// ReadResult is the JSON payload returned for a successful read.
type ReadResult struct {
	Path       string `json:"path"`
	Content    string `json:"content"`
	Offset     int64  `json:"offset"`
	NextOffset int64  `json:"next_offset"`
	SizeBytes  int64  `json:"size_bytes"`
	Truncated  bool   `json:"truncated"`
}

// ReadFile reads one page of a text file inside the session workspace. Path
// canonicalization (ADR-0004) and the sensitive hard-deny table are enforced
// before any content is touched.
type ReadFile struct {
	Root workspace.Root
}

// NewReadFile builds the tool bound to a canonical workspace root.
func NewReadFile(root workspace.Root) *ReadFile {
	return &ReadFile{Root: root}
}

// Meta implements Tool.
func (t *ReadFile) Meta() Meta {
	return Meta{
		Name:        "read_file",
		Ref:         "builtin://read_file",
		Description: "Read a page of a text file inside the workspace and return its content.",
		Parameters:  json.RawMessage(readFileParams),
		Action:      "read",
		Source:      "builtin",
		Trust:       "trusted",
		Reversible:  true,
		RetrySafe:   true,
	}
}

// readArgs is the expected argument object.
type readArgs struct {
	Path     string `json:"path"`
	Offset   int64  `json:"offset"`
	MaxBytes int64  `json:"max_bytes"`
}

// Exec implements Tool.
func (t *ReadFile) Exec(ctx context.Context, args json.RawMessage) (any, error) {
	var a readArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("read_file: bad arguments: %w", err)
	}
	if a.Path == "" {
		return nil, fmt.Errorf("read_file: missing required argument \"path\"")
	}
	if a.Offset < 0 {
		return nil, fmt.Errorf("read_file: offset must be >= 0")
	}
	path, err := t.Root.Resolve(a.Path)
	if err != nil {
		return nil, fmt.Errorf("read_file: %w", err)
	}
	rel, err := t.Root.Rel(path)
	if err != nil {
		return nil, fmt.Errorf("read_file: %w", err)
	}
	if workspace.Sensitive(rel) {
		return nil, fmt.Errorf("read_file: %q: %w", rel, workspace.ErrSensitivePath)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("read_file: stat %s: %w", path, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("read_file: %s is a directory", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read_file: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	limit := a.MaxBytes
	if limit <= 0 {
		limit = DefaultReadFileLimit
	}
	if limit > MaxReadFileLimit {
		limit = MaxReadFileLimit
	}
	offset := a.Offset
	if offset > info.Size() {
		offset = info.Size()
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, fmt.Errorf("read_file: seek %s: %w", path, err)
	}
	buf, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return nil, fmt.Errorf("read_file: read %s: %w", path, err)
	}
	next := offset + int64(len(buf))
	truncated := next < info.Size()
	return ReadResult{
		Path:       path,
		Content:    string(buf),
		Offset:     offset,
		NextOffset: next,
		SizeBytes:  info.Size(),
		Truncated:  truncated,
	}, nil
}
