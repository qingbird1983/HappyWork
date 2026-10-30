package storage

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/happywork/happywork/internal/audit"
	"github.com/happywork/happywork/internal/rollback"
)

// TestAuditWritesSurviveContention is the CI regression for the SQLite locking
// bug that only surfaced on Linux runners. AppendAudit opens a transaction that
// reads the chain head and then writes, i.e. a deferred read->write upgrade;
// with an unbounded connection pool every goroutine got its own connection, and
// the first connection was the only one that ever received the
// busy_timeout/WAL/foreign_keys pragmas. A reader holding a snapshot then makes
// the upgrade return SQLITE_BUSY_SNAPSHOT immediately, which busy_timeout does
// not retry. In the app this is the audit/backup panel polling while the session
// actor appends records.
func TestAuditWritesSurviveContention(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	if err := s.CreateSession(ctx, &Session{ID: "s1", WorkspaceRoot: "ws"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	const (
		writers = 4
		readers = 4
		rounds  = 25
	)

	var wg sync.WaitGroup
	failCh := make(chan error, (writers+readers)*rounds)

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				msg := &Message{
					SessionID: "s1", Role: RoleUser, TurnID: int64(i),
					Content: fmt.Sprintf("w%d/%d", w, i), Trust: TrustTrusted,
				}
				if err := s.AppendMessage(ctx, msg); err != nil {
					failCh <- fmt.Errorf("append message: %w", err)
					return
				}
				rec := audit.Record{
					SessionID: "s1", TurnID: int64(i), Action: audit.ActionToolCall,
					Decision: audit.DecisionAllow, Reason: "contention", Detail: "{}",
				}
				if err := s.AppendAudit(ctx, rec); err != nil {
					failCh <- fmt.Errorf("append audit: %w", err)
					return
				}
				b := rollback.Backup{
					SessionID: "s1", TurnID: int64(i), RelPath: fmt.Sprintf("f%d.txt", w),
					Kind: rollback.KindOverwrite, Reason: rollback.ReasonWrite,
					Mode: 0o600, Size: 4, BackupRel: fmt.Sprintf("s1/f%d.txt/v%d.bak", w, i),
					BackupSHA256: "backup-hash", WrittenSHA256: "written-hash",
				}
				if err := s.CreateFileBackup(ctx, &b); err != nil {
					failCh <- fmt.Errorf("create backup: %w", err)
					return
				}
			}
		}(w)
	}

	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				if _, err := s.ListAudit(ctx, "s1"); err != nil {
					failCh <- fmt.Errorf("list audit: %w", err)
					return
				}
				if _, err := s.VerifyAudit(ctx); err != nil {
					failCh <- fmt.Errorf("verify audit: %w", err)
					return
				}
				if _, err := s.Transcript(ctx, "s1"); err != nil {
					failCh <- fmt.Errorf("transcript: %w", err)
					return
				}
				if _, err := s.ListFileBackups(ctx, "s1"); err != nil {
					failCh <- fmt.Errorf("list backups: %w", err)
					return
				}
			}
		}()
	}

	wg.Wait()
	close(failCh)

	var locked int
	for err := range failCh {
		if strings.Contains(err.Error(), "database is locked") {
			locked++
		}
		t.Errorf("contended store call: %v", err)
	}

	want := writers * rounds
	if res, err := s.VerifyAudit(ctx); err != nil || !res.OK {
		t.Fatalf("verify after contention: %+v, %v", res, err)
	}
	rows, err := s.ListAudit(ctx, "s1")
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(rows) != want {
		t.Fatalf("audit rows = %d, want %d (locked errors: %d)", len(rows), want, locked)
	}
	if locked > 0 {
		t.Fatalf("%d calls failed with SQLITE_BUSY", locked)
	}
}
