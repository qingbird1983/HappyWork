package rollback

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("rollback: newID: %v", err))
	}
	return hex.EncodeToString(b[:])
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// safeBackupPath joins rel under root and refuses absolute/parent traversal.
func safeBackupPath(root, rel string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("rollback: backup root is empty")
	}
	if rel == "" || filepath.IsAbs(rel) {
		return "", fmt.Errorf("rollback: invalid backup path %q", rel)
	}
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("rollback: invalid backup path %q", rel)
	}
	full := filepath.Join(root, clean)
	inside, err := filepath.Rel(root, full)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) || filepath.IsAbs(inside) {
		return "", fmt.Errorf("rollback: backup path escapes root: %q", rel)
	}
	return full, nil
}

// writeAtomic writes content to target via a temp file in the same directory.
func writeAtomic(target, content string, mode os.FileMode) error {
	parent := filepath.Dir(target)
	tmp, err := os.CreateTemp(parent, ".happywork-write-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := io.WriteString(tmp, content); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("fsync temp: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		cleanup()
		return fmt.Errorf("atomic rename: %w", err)
	}
	syncDir(parent)
	return nil
}

// copyAtomic copies src to dst via a temp file and returns the SHA-256 of the
// copied bytes. dstMode is applied to the result (backups use 0600).
func copyAtomic(src, dst string, dstMode os.FileMode) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", fmt.Errorf("open source: %w", err)
	}
	defer func() { _ = in.Close() }()
	parent := filepath.Dir(dst)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", fmt.Errorf("create backup directory: %w", err)
	}
	tmp, err := os.CreateTemp(parent, ".rollback-*")
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), in); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", fmt.Errorf("copy: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", fmt.Errorf("fsync copy: %w", err)
	}
	if err := tmp.Chmod(dstMode); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", fmt.Errorf("chmod copy: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", fmt.Errorf("close copy: %w", err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		cleanup()
		return "", fmt.Errorf("rename copy: %w", err)
	}
	syncDir(parent)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// moveFile renames src to dst, falling back to copy+remove across volumes.
// The fallback copies with 0600 and removes the source only after a fsynced
// copy; any failure leaves the source untouched.
func moveFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return fmt.Errorf("create move directory: %w", err)
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if _, err := copyAtomic(src, dst, 0o600); err != nil {
		return fmt.Errorf("copy across volumes: %w", err)
	}
	if err := os.Remove(src); err != nil {
		return fmt.Errorf("remove source after copy: %w", err)
	}
	return nil
}

func syncDir(dir string) {
	f, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = f.Sync()
	_ = f.Close()
}
