package tool

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// MaxInlineResult is the largest tool result returned verbatim to the model;
// larger results are written to attachments/<session>/<sha256>.txt
// (design.md §6.1 step 8).
const MaxInlineResult = 256 << 10

// WriteAttachment stores an oversized result and returns its path and SHA-256.
// The file is 0600 inside a 0700 session directory. Identical content reuses
// the same file.
func WriteAttachment(dataDir, sessionID string, content []byte) (path, digest string, err error) {
	if dataDir == "" || sessionID == "" {
		return "", "", fmt.Errorf("tool: attachment location not configured")
	}
	sum := sha256.Sum256(content)
	digest = hex.EncodeToString(sum[:])
	dir := filepath.Join(dataDir, "attachments", sessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", fmt.Errorf("tool: attachment dir: %w", err)
	}
	path = filepath.Join(dir, digest+".txt")
	if _, err := os.Stat(path); err == nil {
		return path, digest, nil
	}
	tmp, err := os.CreateTemp(dir, ".attachment-*")
	if err != nil {
		return "", "", fmt.Errorf("tool: attachment temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", "", fmt.Errorf("tool: attachment write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", "", fmt.Errorf("tool: attachment fsync: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", "", fmt.Errorf("tool: attachment chmod: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", "", fmt.Errorf("tool: attachment close: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return "", "", fmt.Errorf("tool: attachment rename: %w", err)
	}
	return path, digest, nil
}
