package rollback

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/happywork/happywork/internal/workspace"
)

type memStore struct {
	mu   sync.Mutex
	rows map[string]Backup
}

func newMemStore() *memStore { return &memStore{rows: map[string]Backup{}} }

func (m *memStore) CreateFileBackup(_ context.Context, b *Backup) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b.ID == "" {
		b.ID = newID()
	}
	if b.CreatedAt == "" {
		b.CreatedAt = nowString(time.Now())
	}
	m.rows[b.ID] = *b
	return nil
}

func (m *memStore) UpdateFileBackupWritten(_ context.Context, id, sha, verifiedAt string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.rows[id]
	if !ok {
		return fmt.Errorf("backup %s not found", id)
	}
	b.WrittenSHA256 = sha
	b.VerifiedAt = verifiedAt
	m.rows[id] = b
	return nil
}

func (m *memStore) MarkFileBackupRestored(_ context.Context, id, discardedRel, restoredAt string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.rows[id]
	if !ok {
		return fmt.Errorf("backup %s not found", id)
	}
	b.DiscardedRel = discardedRel
	b.RestoredAt = restoredAt
	m.rows[id] = b
	return nil
}

func (m *memStore) GetFileBackup(_ context.Context, id string) (Backup, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.rows[id]
	if !ok {
		return Backup{}, fmt.Errorf("backup %s not found", id)
	}
	return b, nil
}

func (m *memStore) LatestFileBackupVersion(_ context.Context, sessionID, relPath string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var max int64
	for _, b := range m.rows {
		if b.SessionID == sessionID && b.RelPath == relPath && b.Version > max {
			max = b.Version
		}
	}
	return max, nil
}

func (m *memStore) ListFileBackups(_ context.Context, sessionID string) ([]Backup, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Backup
	for _, b := range m.rows {
		if b.SessionID == sessionID {
			out = append(out, b)
		}
	}
	return out, nil
}

func (m *memStore) BackupUsage(context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var total int64
	for _, b := range m.rows {
		if b.BackupRel != "" || b.DiscardedRel != "" {
			total += b.Size
		}
	}
	return total, nil
}

func (m *memStore) DeleteFileBackup(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, id)
	return nil
}

func (m *memStore) find(t *testing.T, rel string, version int64) Backup {
	t.Helper()
	rows, _ := m.ListFileBackups(context.Background(), "sess1")
	for _, b := range rows {
		if b.RelPath == rel && b.Version == version {
			return b
		}
	}
	t.Fatalf("backup %s v%d not found", rel, version)
	return Backup{}
}

func newTestWriter(t *testing.T, limits Limits) (*Writer, *memStore, string, string) {
	t.Helper()
	base := t.TempDir()
	wsDir := filepath.Join(base, "ws")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := workspace.NewRoot(wsDir)
	if err != nil {
		t.Fatal(err)
	}
	store := newMemStore()
	backupRoot := filepath.Join(base, "backups")
	w := NewWriter(root, backupRoot, "sess1", store, limits)
	w.now = func() time.Time { return time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC) }
	return w, store, root.String(), backupRoot
}

func TestWriteCreateOverwriteVersions(t *testing.T) {
	w, store, root, _ := newTestWriter(t, DefaultLimits())
	ctx := context.Background()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	first, err := w.Write(ctx, "docs/a.md", "第一版", 0o644)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !first.Created || first.BackupSaved || first.Version != 1 || !first.Verified {
		t.Fatalf("create outcome = %+v", first)
	}
	if err := os.Chmod(filepath.Join(root, "docs", "a.md"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := w.Write(ctx, "docs/a.md", "第二版", 0o644)
	if err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if second.Created || !second.BackupSaved || second.Version != 2 || !second.Verified {
		t.Fatalf("overwrite outcome = %+v", second)
	}
	body, _ := os.ReadFile(filepath.Join(root, "docs", "a.md"))
	if string(body) != "第二版" {
		t.Fatalf("content = %q", body)
	}
	rec := store.find(t, "docs/a.md", 2)
	if rec.Kind != KindOverwrite || rec.BackupSHA256 == "" || rec.WrittenSHA256 == "" || rec.VerifiedAt == "" {
		t.Fatalf("backup record = %+v", rec)
	}
	backup, err := os.ReadFile(filepath.Join(w.backupRoot, rec.BackupRel))
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backup) != "第一版" {
		t.Fatalf("backup content = %q", backup)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(filepath.Join(root, "docs", "a.md"))
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %o", info.Mode().Perm())
		}
	}
	groups, err := w.ListGroups(ctx)
	if err != nil || len(groups) != 1 || len(groups[0].Backups) != 2 {
		t.Fatalf("groups = %+v, %v", groups, err)
	}
}

func TestWriteQuotaRefusesOverwrite(t *testing.T) {
	w, _, root, _ := newTestWriter(t, Limits{MaxVersionsPerFile: 20, SoftQuotaBytes: 10})
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("01234567890123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := w.Write(ctx, "a.txt", "new", 0o644)
	if !errors.Is(err, ErrQuota) {
		t.Fatalf("err = %v, want ErrQuota", err)
	}
	body, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(body) != "01234567890123456789" {
		t.Fatal("target changed despite quota refusal")
	}
}

func TestWriteRetentionPrunes(t *testing.T) {
	w, store, root, _ := newTestWriter(t, Limits{MaxVersionsPerFile: 2, SoftQuotaBytes: 1 << 20})
	ctx := context.Background()
	if _, err := w.Write(ctx, "a.txt", "v1", 0o644); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"v2", "v3", "v4"} {
		if _, err := w.Write(ctx, "a.txt", body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rows, _ := store.ListFileBackups(ctx, "sess1")
	if len(rows) != 2 {
		t.Fatalf("retained %d rows, want 2: %+v", len(rows), rows)
	}
	var versions []int64
	for _, b := range rows {
		versions = append(versions, b.Version)
	}
	if !contains(versions, 3) || !contains(versions, 4) {
		t.Fatalf("versions = %v, want 3,4", versions)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("target missing: %v", err)
	}
}

func TestRestoreCreatesSafetyCopyAndCanBeUndone(t *testing.T) {
	w, store, root, backupRoot := newTestWriter(t, DefaultLimits())
	ctx := context.Background()
	if _, err := w.Write(ctx, "a.txt", "v1", 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(ctx, "a.txt", "v2", 0o644); err != nil {
		t.Fatal(err)
	}
	rec := store.find(t, "a.txt", 2)
	out, err := w.Restore(ctx, rec.ID)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !out.Restored || out.Created || out.SafetyBackupID == "" {
		t.Fatalf("restore outcome = %+v", out)
	}
	body, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(body) != "v1" {
		t.Fatalf("restored content = %q", body)
	}
	safety := store.find(t, "a.txt", 3)
	if safety.Reason != ReasonPreRestore || safety.RestoreOf != rec.ID {
		t.Fatalf("safety = %+v", safety)
	}
	safetyBody, err := os.ReadFile(filepath.Join(backupRoot, safety.BackupRel))
	if err != nil || string(safetyBody) != "v2" {
		t.Fatalf("safety content = %q, %v", safetyBody, err)
	}
	if _, err := w.Restore(ctx, safety.ID); err != nil {
		t.Fatalf("restore safety: %v", err)
	}
	body, _ = os.ReadFile(filepath.Join(root, "a.txt"))
	if string(body) != "v2" {
		t.Fatalf("second restore content = %q", body)
	}
}

func TestRestoreRecreatesDeletedFile(t *testing.T) {
	w, store, root, _ := newTestWriter(t, DefaultLimits())
	ctx := context.Background()
	if _, err := w.Write(ctx, "a.txt", "v1", 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(ctx, "a.txt", "v2", 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
		t.Fatal(err)
	}
	rec := store.find(t, "a.txt", 2)
	out, err := w.Restore(ctx, rec.ID)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !out.Restored || !out.Created {
		t.Fatalf("restore outcome = %+v", out)
	}
	body, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(body) != "v1" {
		t.Fatalf("content = %q", body)
	}
	safety := store.find(t, "a.txt", 3)
	if safety.Kind != KindCreate || safety.Reason != ReasonRestoreCreate {
		t.Fatalf("safety = %+v", safety)
	}
	if _, err := w.UndoCreate(ctx, safety.ID); err != nil {
		t.Fatalf("undo restore create: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(err) {
		t.Fatal("file still exists after undo")
	}
}

func TestUndoCreate(t *testing.T) {
	w, store, root, backupRoot := newTestWriter(t, DefaultLimits())
	ctx := context.Background()
	out, err := w.Write(ctx, "a.txt", "created", 0o644)
	if err != nil {
		t.Fatal(err)
	}
	rec := store.find(t, "a.txt", out.Version)
	undo, err := w.UndoCreate(ctx, rec.ID)
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	if !undo.Restored || undo.AlreadyAbsent {
		t.Fatalf("undo outcome = %+v", undo)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(err) {
		t.Fatal("target still exists")
	}
	rec = store.find(t, "a.txt", out.Version)
	discarded, err := os.ReadFile(filepath.Join(backupRoot, rec.DiscardedRel))
	if err != nil || string(discarded) != "created" {
		t.Fatalf("discarded = %q, %v", discarded, err)
	}
	again, err := w.UndoCreate(ctx, rec.ID)
	if err != nil || !again.AlreadyAbsent {
		t.Fatalf("second undo = %+v, %v", again, err)
	}
}

func TestUndoCreateRejectsModified(t *testing.T) {
	w, store, root, _ := newTestWriter(t, DefaultLimits())
	ctx := context.Background()
	out, err := w.Write(ctx, "a.txt", "created", 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := store.find(t, "a.txt", out.Version)
	if _, err := w.UndoCreate(ctx, rec.ID); !errors.Is(err, ErrModified) {
		t.Fatalf("err = %v, want ErrModified", err)
	}
}

func TestVerifyFailureRollsBack(t *testing.T) {
	w, store, root, _ := newTestWriter(t, DefaultLimits())
	ctx := context.Background()
	if _, err := w.Write(ctx, "a.txt", "v1", 0o644); err != nil {
		t.Fatal(err)
	}
	w.verifyAfterWrite = func(string) (string, error) { return strings.Repeat("0", 64), nil }
	out, err := w.Write(ctx, "a.txt", "v2", 0o644)
	var verifyErr *VerifyError
	if !errors.As(err, &verifyErr) || !verifyErr.RolledBack || out.RolledBack == false {
		t.Fatalf("err = %v, outcome = %+v", err, out)
	}
	body, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(body) != "v1" {
		t.Fatalf("rollback content = %q", body)
	}
	_ = store
}

func TestVerifyFailureOnCreateDiscards(t *testing.T) {
	w, store, root, backupRoot := newTestWriter(t, DefaultLimits())
	ctx := context.Background()
	w.verifyAfterWrite = func(string) (string, error) { return strings.Repeat("0", 64), nil }
	out, err := w.Write(ctx, "new.txt", "content", 0o644)
	var verifyErr *VerifyError
	if !errors.As(err, &verifyErr) || !verifyErr.RolledBack {
		t.Fatalf("err = %v", err)
	}
	if out.Created != true || !out.RolledBack {
		t.Fatalf("outcome = %+v", out)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); !os.IsNotExist(err) {
		t.Fatal("created file should be moved away")
	}
	rec := store.find(t, "new.txt", out.Version)
	if rec.DiscardedRel == "" {
		t.Fatalf("record not marked discarded: %+v", rec)
	}
	discarded, err := os.ReadFile(filepath.Join(backupRoot, rec.DiscardedRel))
	if err != nil || string(discarded) != "content" {
		t.Fatalf("discarded = %q, %v", discarded, err)
	}
}

func TestWriteRejectsSensitiveAndTraversal(t *testing.T) {
	w, _, _, _ := newTestWriter(t, DefaultLimits())
	ctx := context.Background()
	if _, err := w.Write(ctx, ".env", "x", 0o644); !errors.Is(err, workspace.ErrSensitivePath) {
		t.Fatalf("env err = %v", err)
	}
	if _, err := w.Write(ctx, "../escape.txt", "x", 0o644); err == nil {
		t.Fatal("traversal should fail")
	}
	if _, err := w.Write(ctx, "/abs.txt", "x", 0o644); err == nil {
		t.Fatal("absolute path should fail")
	}
}

func TestWriteWithoutStoreFails(t *testing.T) {
	base := t.TempDir()
	wsDir := filepath.Join(base, "ws")
	_ = os.MkdirAll(wsDir, 0o755)
	root, err := workspace.NewRoot(wsDir)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWriter(root, filepath.Join(base, "b"), "s", nil, DefaultLimits())
	if _, err := w.Write(context.Background(), "a.txt", "x", 0o644); err == nil {
		t.Fatal("write without store should fail")
	}
}

func contains(values []int64, want int64) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
