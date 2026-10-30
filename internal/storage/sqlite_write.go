package storage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/happywork/happywork/internal/rollback"
)

// writeTx runs fn inside one transaction. Sequences that read a current value
// and then insert derived from it (message seq, backup version, audit chain
// head) must stay in a single transaction: with the single pooled connection
// there is no second writer to race, and a deferred read->write upgrade can no
// longer return SQLITE_BUSY_SNAPSHOT to the caller.
func (s *sqliteStore) writeTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// nextSeq returns the next transcript sequence for a session inside tx.
func nextSeq(ctx context.Context, tx *sql.Tx, sessionID string) (int64, error) {
	var seq sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT MAX(seq) FROM messages WHERE session_id = ?`, sessionID).Scan(&seq); err != nil {
		return 0, fmt.Errorf("storage: next seq: %w", err)
	}
	return seq.Int64 + 1, nil
}

// touchTx bumps the session's updated_at inside tx, so a transcript write and
// the recency it implies commit together.
func touchTx(ctx context.Context, tx *sql.Tx, sessionID string) error {
	_, err := tx.ExecContext(ctx, `UPDATE sessions SET updated_at = ? WHERE id = ?`, Now(), sessionID)
	return err
}

// AppendMessage writes one transcript entry, assigning its sequence and
// touching the session row in the same transaction.
func (s *sqliteStore) AppendMessage(ctx context.Context, m *Message) error {
	if m.ID == "" {
		m.ID = NewID()
	}
	if m.CreatedAt == "" {
		m.CreatedAt = Now()
	}
	if m.Trust == "" {
		m.Trust = TrustTrusted
	}
	return s.writeTx(ctx, func(tx *sql.Tx) error {
		seq, err := nextSeq(ctx, tx, m.SessionID)
		if err != nil {
			return err
		}
		m.Seq = seq
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO messages (id, session_id, seq, role, content, tool_call_id, tool_name, tool_calls_json, turn_id, trust, created_at)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			m.ID, m.SessionID, m.Seq, m.Role, m.Content, m.ToolCallID, m.ToolName, m.ToolCallsJSON, m.TurnID, m.Trust, m.CreatedAt); err != nil {
			return err
		}
		return touchTx(ctx, tx, m.SessionID)
	})
}

// nextBackupVersion returns the next version for (session, relPath) inside tx.
func nextBackupVersion(ctx context.Context, tx *sql.Tx, sessionID, relPath string) (int64, error) {
	var v sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT MAX(version) FROM file_backups WHERE session_id = ? AND rel_path = ?`,
		sessionID, relPath).Scan(&v); err != nil {
		return 0, fmt.Errorf("storage: next backup version: %w", err)
	}
	return v.Int64 + 1, nil
}

// CreateFileBackup stores one backup record. The writer normally assigns the
// version itself; when it leaves it unset the version is derived inside the
// insert transaction so two callers can never claim the same number.
func (s *sqliteStore) CreateFileBackup(ctx context.Context, b *rollback.Backup) error {
	if b.ID == "" {
		b.ID = NewID()
	}
	if b.CreatedAt == "" {
		b.CreatedAt = Now()
	}
	return s.writeTx(ctx, func(tx *sql.Tx) error {
		if b.Version == 0 {
			v, err := nextBackupVersion(ctx, tx, b.SessionID, b.RelPath)
			if err != nil {
				return err
			}
			b.Version = v
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO file_backups
			 (id, session_id, turn_id, rel_path, kind, reason, version, mode, size,
			  backup_rel, backup_sha256, written_sha256, discarded_rel, restore_of,
			  created_at, verified_at, restored_at)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			b.ID, b.SessionID, b.TurnID, b.RelPath, b.Kind, b.Reason, b.Version,
			int64(b.Mode), b.Size, b.BackupRel, b.BackupSHA256, b.WrittenSHA256,
			b.DiscardedRel, b.RestoreOf, b.CreatedAt, b.VerifiedAt, b.RestoredAt)
		if err != nil {
			return fmt.Errorf("storage: create file backup: %w", err)
		}
		return nil
	})
}
