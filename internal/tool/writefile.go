package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"

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
}

// WriteFile writes UTF-8 text inside the workspace with a temp-file + fsync +
// atomic-rename sequence. Overwrites are copied to BackupRoot first; the tool
// refuses to overwrite when no backup location is configured.
type WriteFile struct {
	Root       workspace.Root
	BackupRoot string // app data .../backups
	SessionID  string
}

// NewWriteFile builds the tool bound to one session.
func NewWriteFile(root workspace.Root, backupRoot, sessionID string) *WriteFile {
	return &WriteFile{Root: root, BackupRoot: backupRoot, SessionID: sessionID}
}

// Meta implements Tool.
func (t *WriteFile) Meta() Meta {
	return Meta{
		Name:        "write_file",
		Ref:         "builtin://write_file",
		Description: "Write UTF-8 text to a file inside the workspace. Overwrites keep a backup copy. Asking the user for approval is handled by the host.",
		Parameters:  json.RawMessage(writeFileParams),
		Action:      "write",
		Source:      "builtin",
		Trust:       "trusted",
		Reversible:  true,
		RetrySafe:   false,
	}
}

type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Exec implements Tool.
func (t *WriteFile) Exec(_ context.Context, args json.RawMessage) (any, error) {
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
	parent := filepath.Dir(target)
	parentInfo, err := os.Stat(parent)
	if err != nil {
		return nil, fmt.Errorf("write_file: parent directory %s: %w", parent, err)
	}
	if !parentInfo.IsDir() {
		return nil, fmt.Errorf("write_file: parent %s is not a directory", parent)
	}

	existing, statErr := os.Stat(target)
	if statErr == nil && existing.IsDir() {
		return nil, fmt.Errorf("write_file: %s is a directory", target)
	}
	created := os.IsNotExist(statErr)
	mode := os.FileMode(0o644)
	if !created {
		if statErr != nil {
			return nil, fmt.Errorf("write_file: stat %s: %w", target, statErr)
		}
		mode = existing.Mode().Perm()
		if t.BackupRoot == "" || t.SessionID == "" {
			return nil, fmt.Errorf("write_file: refusing to overwrite without a backup location")
		}
		if err := copyToBackup(target, t.BackupRoot, t.SessionID, rel, mode); err != nil {
			return nil, fmt.Errorf("write_file: backup %s: %w", rel, err)
		}
	}

	tmp, err := os.CreateTemp(parent, ".happywork-write-*")
	if err != nil {
		return nil, fmt.Errorf("write_file: temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := io.WriteString(tmp, a.Content); err != nil {
		_ = tmp.Close()
		cleanup()
		return nil, fmt.Errorf("write_file: write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return nil, fmt.Errorf("write_file: fsync temp: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		cleanup()
		return nil, fmt.Errorf("write_file: chmod temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return nil, fmt.Errorf("write_file: close temp: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		cleanup()
		return nil, fmt.Errorf("write_file: atomic rename: %w", err)
	}
	return WriteResult{
		Path:         target,
		BytesWritten: len(a.Content),
		Created:      created,
		BackupSaved:  !created,
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

// copyToBackup snapshots the original file under backups/<session>/<rel>.
func copyToBackup(src, backupRoot, sessionID, rel string, mode os.FileMode) error {
	backupPath := filepath.Join(backupRoot, sessionID, rel)
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	tmp, err := os.CreateTemp(filepath.Dir(backupPath), ".backup-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, backupPath); err != nil {
		cleanup()
		return err
	}
	return nil
}
