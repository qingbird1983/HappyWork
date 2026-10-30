package rollback

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ListGroups returns versions grouped by file, newest version first.
func (w *Writer) ListGroups(ctx context.Context) ([]BackupGroup, error) {
	if w.store == nil {
		return nil, fmt.Errorf("rollback: backup store is not configured")
	}
	rows, err := w.store.ListFileBackups(ctx, w.sessionID)
	if err != nil {
		return nil, err
	}
	byPath := map[string][]Backup{}
	for _, b := range rows {
		byPath[b.RelPath] = append(byPath[b.RelPath], b)
	}
	groups := make([]BackupGroup, 0, len(byPath))
	for rel, list := range byPath {
		sort.Slice(list, func(i, j int) bool { return list[i].Version > list[j].Version })
		groups = append(groups, BackupGroup{RelPath: rel, Backups: list})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].RelPath < groups[j].RelPath })
	return groups, nil
}

// Restore writes a selected overwrite version back to the workspace. When the
// target exists it first creates a pre_restore safety copy, so the restore
// itself can be undone.
func (w *Writer) Restore(ctx context.Context, backupID string) (RestoreOutcome, error) {
	if w.store == nil || w.backupRoot == "" || w.sessionID == "" {
		return RestoreOutcome{}, fmt.Errorf("rollback: backup store/root/session is not configured")
	}
	rec, err := w.store.GetFileBackup(ctx, backupID)
	if err != nil {
		return RestoreOutcome{}, err
	}
	if rec.Kind != KindOverwrite || rec.BackupRel == "" {
		return RestoreOutcome{}, ErrNotRestorable
	}
	backupPath, err := safeBackupPath(w.backupRoot, rec.BackupRel)
	if err != nil {
		return RestoreOutcome{}, err
	}
	backupSHA, err := hashFile(backupPath)
	if err != nil {
		return RestoreOutcome{}, fmt.Errorf("rollback: read backup: %w", err)
	}
	if backupSHA != rec.BackupSHA256 {
		return RestoreOutcome{}, fmt.Errorf("rollback: backup hash mismatch for %s", rec.RelPath)
	}
	target, err := w.resolveTarget(rec.RelPath)
	if err != nil {
		return RestoreOutcome{}, err
	}
	mode := fileMode(rec.Mode)
	if mode == 0 {
		mode = 0o644
	}
	info, statErr := os.Lstat(target)
	targetExists := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return RestoreOutcome{}, fmt.Errorf("rollback: stat target: %w", statErr)
	}
	if targetExists {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return RestoreOutcome{}, fmt.Errorf("rollback: target is not a regular file")
		}
	}
	parent := filepath.Dir(target)
	if parentInfo, err := os.Stat(parent); err != nil || !parentInfo.IsDir() {
		return RestoreOutcome{}, fmt.Errorf("rollback: parent directory is not available")
	}

	turn := turnID(ctx)
	var safety Backup
	if targetExists {
		safety, err = w.makeBackup(ctx, rec.RelPath, target, info.Mode().Perm(), ReasonPreRestore, rec.ID, turn)
		if err != nil {
			return RestoreOutcome{}, err
		}
	} else {
		version, err := w.nextVersion(ctx, rec.RelPath)
		if err != nil {
			return RestoreOutcome{}, err
		}
		safety = Backup{
			ID: newID(), SessionID: w.sessionID, TurnID: turn, RelPath: rec.RelPath,
			Kind: KindCreate, Reason: ReasonRestoreCreate, Version: version,
			Mode: uint32(mode.Perm()), Size: rec.Size, WrittenSHA256: rec.BackupSHA256,
			RestoreOf: rec.ID, CreatedAt: w.timestamp(),
		}
		if err := w.store.CreateFileBackup(ctx, &safety); err != nil {
			return RestoreOutcome{}, fmt.Errorf("rollback: record restore target: %w", err)
		}
	}

	writtenSHA, err := copyAtomic(backupPath, target, mode)
	if err != nil {
		return RestoreOutcome{}, fmt.Errorf("rollback: restore copy: %w", err)
	}
	if writtenSHA != rec.BackupSHA256 {
		rolled := w.rollbackRestore(rec, target, safety, targetExists, mode)
		return RestoreOutcome{Restored: false, RelPath: rec.RelPath, SafetyBackupID: safety.ID},
			&VerifyError{RelPath: rec.RelPath, Expected: rec.BackupSHA256, Actual: writtenSHA, RolledBack: rolled}
	}
	if err := w.store.MarkFileBackupRestored(ctx, rec.ID, "", w.timestamp()); err != nil {
		return RestoreOutcome{}, fmt.Errorf("rollback: mark restored: %w", err)
	}
	return RestoreOutcome{
		BackupID: rec.ID, RelPath: rec.RelPath, Restored: true, Created: !targetExists,
		SafetyBackupID: safety.ID, SHA256: rec.BackupSHA256,
	}, nil
}

func (w *Writer) rollbackRestore(rec Backup, target string, safety Backup, targetExisted bool, mode os.FileMode) bool {
	if targetExisted && safety.BackupRel != "" {
		backupPath, err := safeBackupPath(w.backupRoot, safety.BackupRel)
		if err != nil {
			return false
		}
		_, err = copyAtomic(backupPath, target, mode)
		return err == nil
	}
	if !targetExisted {
		discarded, err := w.discardTarget(rec.RelPath, target, safety.ID)
		if err != nil {
			return false
		}
		_ = w.store.MarkFileBackupRestored(context.Background(), safety.ID, discarded, w.timestamp())
		return true
	}
	return false
}

// UndoCreate moves a file that is still unchanged since creation into the
// backup area. It never unlinks content.
func (w *Writer) UndoCreate(ctx context.Context, backupID string) (RestoreOutcome, error) {
	if w.store == nil || w.backupRoot == "" || w.sessionID == "" {
		return RestoreOutcome{}, fmt.Errorf("rollback: backup store/root/session is not configured")
	}
	rec, err := w.store.GetFileBackup(ctx, backupID)
	if err != nil {
		return RestoreOutcome{}, err
	}
	if rec.Kind != KindCreate || rec.WrittenSHA256 == "" {
		return RestoreOutcome{}, ErrNotRestorable
	}
	target, err := w.resolveTarget(rec.RelPath)
	if err != nil {
		return RestoreOutcome{}, err
	}
	info, statErr := os.Lstat(target)
	if os.IsNotExist(statErr) {
		if rec.RestoredAt == "" {
			_ = w.store.MarkFileBackupRestored(ctx, rec.ID, "", w.timestamp())
		}
		return RestoreOutcome{BackupID: rec.ID, RelPath: rec.RelPath, Restored: true, AlreadyAbsent: true}, nil
	}
	if statErr != nil {
		return RestoreOutcome{}, fmt.Errorf("rollback: stat created file: %w", statErr)
	}
	if rec.RestoredAt != "" {
		return RestoreOutcome{}, ErrAlreadyRestored
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return RestoreOutcome{}, fmt.Errorf("rollback: created target is not a regular file")
	}
	current, err := hashFile(target)
	if err != nil {
		return RestoreOutcome{}, fmt.Errorf("rollback: hash created file: %w", err)
	}
	if current != rec.WrittenSHA256 {
		return RestoreOutcome{}, ErrModified
	}
	discarded, err := w.discardTarget(rec.RelPath, target, rec.ID)
	if err != nil {
		return RestoreOutcome{}, err
	}
	if err := w.store.MarkFileBackupRestored(ctx, rec.ID, discarded, w.timestamp()); err != nil {
		return RestoreOutcome{}, fmt.Errorf("rollback: mark undo: %w", err)
	}
	return RestoreOutcome{BackupID: rec.ID, RelPath: rec.RelPath, Restored: true, SHA256: current}, nil
}
