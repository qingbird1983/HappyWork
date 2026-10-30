package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/happywork/happywork/internal/workspace"
)

// DefaultReadFileLimit caps one read at 2 MiB (design.md §8).
const DefaultReadFileLimit = 2 << 20

// readFileParams is the JSON schema advertised to providers.
const readFileParams = `{
  "type": "object",
  "properties": {
    "path": { "type": "string", "description": "File path, absolute or relative to the workspace root" }
  },
  "required": ["path"]
}`

// ReadResult is the JSON payload returned for a successful read.
type ReadResult struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	SizeBytes int64  `json:"size_bytes"`
	Truncated bool   `json:"truncated"`
}

// ReadFile reads one file inside the session workspace. Phase 1: the only
// gate is the ADR-0004 canonicalization + boundary check; audit trails for
// denials are written by the agent loop (design.md §8).
type ReadFile struct {
	Root     workspace.Root
	MaxBytes int64
}

// NewReadFile builds the tool with the default size limit applied when limit <= 0.
func NewReadFile(root workspace.Root, limit int64) *ReadFile {
	if limit <= 0 {
		limit = DefaultReadFileLimit
	}
	return &ReadFile{Root: root, MaxBytes: limit}
}

// Meta implements Tool.
func (t *ReadFile) Meta() Meta {
	return Meta{
		Name:        "read_file",
		Ref:         "builtin://read_file",
		Description: "Read a text file inside the workspace and return its content.",
		Parameters:  json.RawMessage(readFileParams),
		Reversible:  true,
		RetrySafe:   true,
	}
}

// readArgs is the expected argument object.
type readArgs struct {
	Path string `json:"path"`
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
	path, err := t.Root.Resolve(a.Path)
	if err != nil {
		return nil, fmt.Errorf("read_file: %w", err)
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

	limit := t.MaxBytes
	buf, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read_file: read %s: %w", path, err)
	}
	truncated := int64(len(buf)) > limit
	if truncated {
		buf = buf[:limit]
	}
	return ReadResult{
		Path:      path,
		Content:   string(buf),
		SizeBytes: info.Size(),
		Truncated: truncated || info.Size() > limit,
	}, nil
}
