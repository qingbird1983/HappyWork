package rollback

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/happywork/happywork/internal/workspace"
)

// Writer is bound to one session and workspace root. All writes go through it
// so backup metadata and the post-write check cannot be bypassed by callers.
type Writer struct {
	root       workspace.Root
	backupRoot string
	sessionID  string
	store      Store
	limits     Limits
	now        func() time.Time
	// verifyAfterWrite is a test hook; production uses hashFile.
	verifyAfterWrite func(path string) (string, error)
}

// NewWriter builds a per-session writer. A missing store or backup root makes
// every write fail closed: Phase 4 never writes without a recoverable backup
// record.
func NewWriter(root workspace.Root, backupRoot, sessionID string, store Store, limits Limits) *Writer {
	return &Writer{
		root:       root,
		backupRoot: backupRoot,
		sessionID:  sessionID,
		store:      store,
		limits:     limits.normalized(),
		now:        time.Now,
	}
}

func (w *Writer) timestamp() string {
	if w.now == nil {
		return nowString(time.Now())
	}
	return nowString(w.now())
}

// Write atomically writes content to rel, creating a versioned backup first
// when the file exists. It re-reads and hashes the target; a mismatch rolls
// the target back and returns *VerifyError.
func (w *Writer) Write(ctx context.Context, rel, content string, mode os.FileMode) (WriteOutcome, error) {
	if w.store == nil || w.backupRoot == "" || w.sessionID == "" {
		return WriteOutcome{}, fmt.Errorf("rollback: backup store/root/session is not configured")
	}
	target, err := w.resolveTarget(rel)
	if err != nil {
		return WriteOutcome{}, err
	}
	info, statErr := os.Stat(target)
	created := os.IsNotExist(statErr)
	if statErr != nil && !created {
		return WriteOutcome{}, fmt.Errorf("rollback: stat %s: %w", rel, statErr)
	}
	if !created && info.IsDir() {
		return WriteOutcome{}, fmt.Errorf("rollback: %s is a directory", rel)
	}
	actualMode := mode.Perm()
	if actualMode == 0 {
		actualMode = 0o644
	}
	if !created {
		actualMode = info.Mode().Perm()
	}
	parent := filepath.Dir(target)
	parentInfo, err := os.Stat(parent)
	if err != nil || !parentInfo.IsDir() {
		return WriteOutcome{}, fmt.Errorf("rollback: parent directory %s is not available", parent)
	}

	writtenSHA := hashContent(content)
	turn := turnID(ctx)
	var record Backup
	if created {
		record, err = w.createRecord(ctx, rel, turn, actualMode, int64(len(content)), writtenSHA)
	} else {
		record, err = w.makeBackup(ctx, rel, target, actualMode, ReasonWrite, "", turn)
	}
	if err != nil {
		return WriteOutcome{}, err
	}

	if err := writeAtomic(target, content, actualMode); err != nil {
		return WriteOutcome{}, err
	}
	actualSHA := writtenSHA
	verify := w.verifyAfterWrite
	if verify == nil {
		verify = hashFile
	}
	got, verifyErr := verify(target)
	if verifyErr != nil || got != writtenSHA {
		rolled := w.rollbackAfterFailure(rel, target, record, actualMode)
		return WriteOutcome{
			Path: target, BytesWritten: len(content), Created: created,
			BackupSaved: !created, BackupID: record.ID, Version: record.Version,
			SHA256: actualSHA, Verified: false, RolledBack: rolled,
		}, &VerifyError{RelPath: rel, Expected: writtenSHA, Actual: got, RolledBack: rolled, Cause: verifyErr}
	}
	if err := w.store.UpdateFileBackupWritten(ctx, record.ID, writtenSHA, w.timestamp()); err != nil {
		return WriteOutcome{}, fmt.Errorf("rollback: record verified write: %w", err)
	}
	w.prune(ctx, rel)
	return WriteOutcome{
		Path: target, BytesWritten: len(content), Created: created,
		BackupSaved: !created, BackupID: record.ID, Version: record.Version,
		SHA256: writtenSHA, Verified: true,
	}, nil
}

func (w *Writer) createRecord(ctx context.Context, rel string, turn int64, mode os.FileMode, size int64, writtenSHA string) (Backup, error) {
	version, err := w.nextVersion(ctx, rel)
	if err != nil {
		return Backup{}, err
	}
	record := Backup{
		ID: newID(), SessionID: w.sessionID, TurnID: turn, RelPath: rel,
		Kind: KindCreate, Reason: ReasonWrite, Version: version,
		Mode: uint32(mode.Perm()), Size: size, WrittenSHA256: writtenSHA,
		CreatedAt: w.timestamp(),
	}
	if err := w.store.CreateFileBackup(ctx, &record); err != nil {
		return Backup{}, fmt.Errorf("rollback: record create: %w", err)
	}
	return record, nil
}

func (w *Writer) makeBackup(ctx context.Context, rel, target string, mode os.FileMode, reason, restoreOf string, turn int64) (Backup, error) {
	info, err := os.Stat(target)
	if err != nil {
		return Backup{}, fmt.Errorf("rollback: stat %s: %w", rel, err)
	}
	usage, err := w.store.BackupUsage(ctx)
	if err != nil {
		return Backup{}, fmt.Errorf("rollback: backup usage: %w", err)
	}
	if usage+info.Size() > w.limits.SoftQuotaBytes {
		return Backup{}, ErrQuota
	}
	version, err := w.nextVersion(ctx, rel)
	if err != nil {
		return Backup{}, err
	}
	id := newID()
	backupRel := filepath.Join(w.sessionID, rel, fmt.Sprintf("v%06d-%s.bak", version, id))
	backupPath, err := safeBackupPath(w.backupRoot, backupRel)
	if err != nil {
		return Backup{}, err
	}
	sha, err := copyAtomic(target, backupPath, 0o600)
	if err != nil {
		return Backup{}, fmt.Errorf("rollback: copy backup: %w", err)
	}
	record := Backup{
		ID: id, SessionID: w.sessionID, TurnID: turn, RelPath: rel,
		Kind: KindOverwrite, Reason: reason, Version: version,
		Mode: uint32(mode.Perm()), Size: info.Size(), BackupRel: backupRel,
		BackupSHA256: sha, RestoreOf: restoreOf, CreatedAt: w.timestamp(),
	}
	if err := w.store.CreateFileBackup(ctx, &record); err != nil {
		_ = os.Remove(backupPath)
		return Backup{}, fmt.Errorf("rollback: record backup: %w", err)
	}
	return record, nil
}

func (w *Writer) nextVersion(ctx context.Context, rel string) (int64, error) {
	last, err := w.store.LatestFileBackupVersion(ctx, w.sessionID, rel)
	if err != nil {
		return 0, fmt.Errorf("rollback: latest version: %w", err)
	}
	return last + 1, nil
}

func (w *Writer) rollbackAfterFailure(rel, target string, record Backup, mode os.FileMode) bool {
	if record.Kind == KindCreate {
		discarded, err := w.discardTarget(rel, target, record.ID)
		if err != nil {
			return false
		}
		_ = w.store.MarkFileBackupRestored(context.Background(), record.ID, discarded, w.timestamp())
		return true
	}
	if record.BackupRel == "" {
		return false
	}
	backupPath, err := safeBackupPath(w.backupRoot, record.BackupRel)
	if err != nil {
		return false
	}
	if _, err := copyAtomic(backupPath, target, mode); err != nil {
		return false
	}
	return true
}

func (w *Writer) discardTarget(rel, target, id string) (string, error) {
	discardedRel := filepath.Join(w.sessionID, "discarded", rel, id+".trash")
	dst, err := safeBackupPath(w.backupRoot, discardedRel)
	if err != nil {
		return "", err
	}
	if err := moveFile(target, dst); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return discardedRel, nil
}

func (w *Writer) prune(ctx context.Context, rel string) {
	rows, err := w.store.ListFileBackups(ctx, w.sessionID)
	if err != nil {
		return
	}
	var versions []Backup
	for _, b := range rows {
		if b.RelPath == rel {
			versions = append(versions, b)
		}
	}
	if len(versions) <= w.limits.MaxVersionsPerFile {
		return
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].Version > versions[j].Version })
	for _, b := range versions[w.limits.MaxVersionsPerFile:] {
		// Keep undo-create copies: they are the only remaining copy of content
		// the user explicitly removed from the workspace.
		if b.DiscardedRel != "" {
			continue
		}
		if b.BackupRel != "" {
			if path, err := safeBackupPath(w.backupRoot, b.BackupRel); err == nil {
				_ = os.Remove(path)
			}
		}
		_ = w.store.DeleteFileBackup(ctx, b.ID)
	}
}

func (w *Writer) resolveTarget(rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) {
		return "", fmt.Errorf("rollback: invalid relative path %q", rel)
	}
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("rollback: invalid relative path %q", rel)
	}
	target, err := w.root.Resolve(clean)
	if err != nil {
		return "", fmt.Errorf("rollback: %w", err)
	}
	again, err := w.root.Rel(target)
	if err != nil || !samePath(again, clean) {
		return "", fmt.Errorf("rollback: path changed during resolution")
	}
	if workspace.Sensitive(again) {
		return "", fmt.Errorf("rollback: %q: %w", again, workspace.ErrSensitivePath)
	}
	return target, nil
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if filepath.Separator == '\\' {
		return strings.EqualFold(a, b)
	}
	return a == b
}
