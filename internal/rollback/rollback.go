// Package rollback owns versioned file backups, restore/undo and post-write
// verification (ADR-0016). It is a leaf package: storage implements its Store
// interface, and it only depends on workspace and the standard library.
package rollback

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

// Backup kinds and reasons.
const (
	KindOverwrite = "overwrite"
	KindCreate    = "create"

	ReasonWrite         = "write"
	ReasonPreRestore    = "pre_restore"
	ReasonRestoreCreate = "restore_create"
)

// Defaults bound the backup area. They are deliberately conservative for a
// single-user desktop app; tests can pass smaller Limits.
const (
	DefaultMaxVersionsPerFile = 20
	DefaultSoftQuotaBytes     = int64(512 << 20)
)

// Errors returned to the UI/tool layer.
var (
	ErrQuota           = errors.New("rollback: backup quota exceeded; delete old backups first")
	ErrModified        = errors.New("rollback: file changed since it was created")
	ErrNotRestorable   = errors.New("rollback: this backup cannot be restored")
	ErrAlreadyRestored = errors.New("rollback: this backup was already restored")
)

// Backup is one file version metadata row. Paths are relative to the backup
// root so the database is not tied to an absolute app-data location.
type Backup struct {
	ID            string `json:"id"`
	SessionID     string `json:"sessionId"`
	TurnID        int64  `json:"turnId"`
	RelPath       string `json:"relPath"`
	Kind          string `json:"kind"`
	Reason        string `json:"reason"`
	Version       int64  `json:"version"`
	Mode          uint32 `json:"mode"`
	Size          int64  `json:"size"`
	BackupRel     string `json:"backupRel,omitempty"`
	BackupSHA256  string `json:"backupSha256,omitempty"`
	WrittenSHA256 string `json:"writtenSha256,omitempty"`
	DiscardedRel  string `json:"discardedRel,omitempty"`
	RestoreOf     string `json:"restoreOf,omitempty"`
	CreatedAt     string `json:"createdAt"`
	VerifiedAt    string `json:"verifiedAt,omitempty"`
	RestoredAt    string `json:"restoredAt,omitempty"`
}

// BackupGroup collects versions of one workspace-relative path for the UI.
type BackupGroup struct {
	RelPath string   `json:"relPath"`
	Backups []Backup `json:"backups"`
}

// Store is the persistence subset rollback needs. storage.Store implements it.
type Store interface {
	CreateFileBackup(ctx context.Context, b *Backup) error
	UpdateFileBackupWritten(ctx context.Context, id, writtenSHA, verifiedAt string) error
	MarkFileBackupRestored(ctx context.Context, id, discardedRel, restoredAt string) error
	GetFileBackup(ctx context.Context, id string) (Backup, error)
	LatestFileBackupVersion(ctx context.Context, sessionID, relPath string) (int64, error)
	ListFileBackups(ctx context.Context, sessionID string) ([]Backup, error)
	BackupUsage(ctx context.Context) (int64, error)
	DeleteFileBackup(ctx context.Context, id string) error
}

// Limits bounds retention and disk usage.
type Limits struct {
	MaxVersionsPerFile int
	SoftQuotaBytes     int64
}

// DefaultLimits returns the Phase 4 defaults.
func DefaultLimits() Limits {
	return Limits{MaxVersionsPerFile: DefaultMaxVersionsPerFile, SoftQuotaBytes: DefaultSoftQuotaBytes}
}

func (l Limits) normalized() Limits {
	if l.MaxVersionsPerFile <= 0 {
		l.MaxVersionsPerFile = DefaultMaxVersionsPerFile
	}
	if l.SoftQuotaBytes <= 0 {
		l.SoftQuotaBytes = DefaultSoftQuotaBytes
	}
	return l
}

// WriteOutcome is returned after a successful atomic write + verification.
type WriteOutcome struct {
	Path         string `json:"path"`
	BytesWritten int    `json:"bytesWritten"`
	Created      bool   `json:"created"`
	BackupSaved  bool   `json:"backupSaved"`
	BackupID     string `json:"backupId,omitempty"`
	Version      int64  `json:"version"`
	SHA256       string `json:"sha256"`
	Verified     bool   `json:"verified"`
	RolledBack   bool   `json:"rolledBack,omitempty"`
}

// RestoreOutcome is returned by Restore and UndoCreate.
type RestoreOutcome struct {
	BackupID       string `json:"backupId"`
	RelPath        string `json:"relPath"`
	Restored       bool   `json:"restored"`
	Created        bool   `json:"created,omitempty"`
	AlreadyAbsent  bool   `json:"alreadyAbsent,omitempty"`
	SafetyBackupID string `json:"safetyBackupId,omitempty"`
	SHA256         string `json:"sha256,omitempty"`
}

// VerifyError reports a post-write content mismatch. RolledBack tells whether
// the target was returned to its pre-write state.
type VerifyError struct {
	RelPath    string
	Expected   string
	Actual     string
	RolledBack bool
	Cause      error
}

func (e *VerifyError) Error() string {
	msg := fmt.Sprintf("rollback: post-write verification failed for %s", e.RelPath)
	if e.RolledBack {
		msg += " (rolled back)"
	}
	if e.Cause != nil {
		return msg + ": " + e.Cause.Error()
	}
	return msg
}

func (e *VerifyError) Unwrap() error { return e.Cause }

// TurnID is carried through the tool context so one write is linked to the
// audit/transcript turn without adding a parameter to the Tool interface.
type turnKey struct{}

// WithTurn attaches a turn ID to ctx.
func WithTurn(ctx context.Context, turnID int64) context.Context {
	return context.WithValue(ctx, turnKey{}, turnID)
}

func turnID(ctx context.Context) int64 {
	if ctx == nil {
		return 0
	}
	if id, ok := ctx.Value(turnKey{}).(int64); ok {
		return id
	}
	return 0
}

func nowString(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func fileMode(m uint32) os.FileMode { return os.FileMode(m).Perm() }
