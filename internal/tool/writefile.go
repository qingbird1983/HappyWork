package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"unicode/utf8"

	"github.com/happywork/happywork/internal/rollback"
	"github.com/happywork/happywork/internal/workspace"
)

const (
	// MaxWriteFileBytes caps one write (design.md §6.2).
	MaxWriteFileBytes = 2 << 20

	writeFileParams = `{
  "type": "object",
  "properties": {
    "path": { "type": "string", "description": "File path relative to the workspace root" },
    "content": { "type": "string", "description": "Full new file content (UTF-8 text, no NUL bytes, max 2 MiB)" }
  },
  "required": ["path", "content"]
}`
)

// WriteResult is the JSON payload returned for a successful write.
type WriteResult struct {
	Path         string `json:"path"`
	BytesWritten int    `json:"bytes_written"`
	Created      bool   `json:"created"`
	BackupSaved  bool   `json:"backup_saved"`
	BackupID     string `json:"backup_id,omitempty"`
	Version      int64  `json:"version,omitempty"`
	SHA256       string `json:"sha256,omitempty"`
	Verified     bool   `json:"verified,omitempty"`
	RolledBack   bool   `json:"rolled_back,omitempty"`
}

type fileWriter interface {
	Write(ctx context.Context, rel, content string, mode os.FileMode) (rollback.WriteOutcome, error)
}

// WriteFile validates arguments and delegates the recoverable write pipeline
// to a per-session rollback.Writer.
type WriteFile struct {
	Root   workspace.Root
	writer fileWriter
}

// NewWriteFile builds the tool bound to one session.
func NewWriteFile(root workspace.Root, writer fileWriter) *WriteFile {
	return &WriteFile{Root: root, writer: writer}
}

// Meta implements Tool.
func (t *WriteFile) Meta() Meta {
	return Meta{
		Name:        "write_file",
		Ref:         "builtin://write_file",
		Description: "Write UTF-8 text to a file inside the workspace. Overwrites keep a versioned backup and are verified after writing. Asking the user for approval is handled by the host.",
		Parameters:  json.RawMessage(writeFileParams),
		Action:      "write",
		Source:      "builtin",
		Trust:       "trusted",
		Reversible:  true,
		RetrySafe:   false,
		// Phase 3 allows hooks to rewrite the body only; path stays immutable.
		PatchableFields: []string{"content"},
	}
}

type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Exec implements Tool.
func (t *WriteFile) Exec(ctx context.Context, args json.RawMessage) (any, error) {
	var a writeArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("write_file: bad arguments: %w", err)
	}
	if a.Path == "" {
		return nil, fmt.Errorf("write_file: missing required argument \"path\"")
	}
	if len(a.Content) > MaxWriteFileBytes {
		return nil, fmt.Errorf("write_file: content is %d bytes, limit is %d", len(a.Content), MaxWriteFileBytes)
	}
	if !utf8.ValidString(a.Content) {
		return nil, fmt.Errorf("write_file: content is not valid UTF-8 text")
	}
	if containsNUL(a.Content) {
		return nil, fmt.Errorf("write_file: content contains NUL bytes (binary files are not supported)")
	}
	if t.writer == nil {
		return nil, fmt.Errorf("write_file: backup writer is not configured")
	}
	target, err := t.Root.Resolve(a.Path)
	if err != nil {
		return nil, fmt.Errorf("write_file: %w", err)
	}
	rel, err := t.Root.Rel(target)
	if err != nil {
		return nil, fmt.Errorf("write_file: %w", err)
	}
	if workspace.Sensitive(rel) {
		return nil, fmt.Errorf("write_file: %q: %w", rel, workspace.ErrSensitivePath)
	}
	out, err := t.writer.Write(ctx, rel, a.Content, 0o644)
	if err != nil {
		return nil, fmt.Errorf("write_file: %w", err)
	}
	return WriteResult{
		Path:         out.Path,
		BytesWritten: out.BytesWritten,
		Created:      out.Created,
		BackupSaved:  out.BackupSaved,
		BackupID:     out.BackupID,
		Version:      out.Version,
		SHA256:       out.SHA256,
		Verified:     out.Verified,
		RolledBack:   out.RolledBack,
	}, nil
}

func containsNUL(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			return true
		}
	}
	return false
}
