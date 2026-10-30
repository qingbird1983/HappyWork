package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (ADR-0010); registered as "sqlite"

	"github.com/happywork/happywork/internal/audit"
	"github.com/happywork/happywork/internal/rollback"
	"github.com/happywork/happywork/migrations"
)

// schemaVersionKey records the highest applied migration in the kv table.
const schemaVersionKey = "schema_version"

// NewID returns a random 128-bit hex identifier.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("storage: NewID: %v", err))
	}
	return hex.EncodeToString(b[:])
}

// Open creates or opens the SQLite database at path, applying pending
// migrations idempotently. The caller must Close it.
func Open(path string) (Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("storage: open %s: %w", path, err)
	}
	// WAL + busy timeout: single writer with short concurrent reads.
	for _, pragma := range []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("storage: %s: %w", pragma, err)
		}
	}
	s := &sqliteStore{db: db}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

type sqliteStore struct {
	db      *sql.DB
	auditMu sync.Mutex
}

func (s *sqliteStore) Close() error { return s.db.Close() }

func (s *sqliteStore) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS kv (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("storage: bootstrap kv: %w", err)
	}
	applied, err := s.KVGet(ctx, schemaVersionKey)
	if err != nil {
		return err
	}
	stmts, err := migrations.Ordered()
	if err != nil {
		return err
	}
	for i, st := range stmts {
		version := fmt.Sprint(i + 1)
		if applied == version {
			continue
		}
		if applied != "" && applied >= version {
			continue // already applied in a previous run
		}
		if _, err := s.db.ExecContext(ctx, st.SQL); err != nil {
			return fmt.Errorf("storage: apply migration %s: %w", st.Name, err)
		}
		if err := s.KVSet(ctx, schemaVersionKey, version); err != nil {
			return err
		}
		applied = version
	}
	return nil
}

func (s *sqliteStore) CreateSession(ctx context.Context, sess *Session) error {
	if sess.ID == "" {
		sess.ID = NewID()
	}
	if sess.CreatedAt == "" {
		sess.CreatedAt = Now()
	}
	sess.UpdatedAt = sess.CreatedAt
	if sess.State == "" {
		sess.State = "idle"
	}
	if sess.Preset == "" {
		sess.Preset = "default"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, title, workspace_root, model_id, preset, state, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?)`,
		sess.ID, sess.Title, sess.WorkspaceRoot, sess.ModelID, sess.Preset, sess.State, sess.CreatedAt, sess.UpdatedAt)
	return err
}

func (s *sqliteStore) ListSessions(ctx context.Context) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, workspace_root, model_id, preset, state, created_at, updated_at FROM sessions ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Session
	for rows.Next() {
		var sess Session
		if err := rows.Scan(&sess.ID, &sess.Title, &sess.WorkspaceRoot, &sess.ModelID, &sess.Preset, &sess.State, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

func (s *sqliteStore) GetSession(ctx context.Context, id string) (Session, error) {
	var sess Session
	err := s.db.QueryRowContext(ctx,
		`SELECT id, title, workspace_root, model_id, preset, state, created_at, updated_at FROM sessions WHERE id = ?`, id).
		Scan(&sess.ID, &sess.Title, &sess.WorkspaceRoot, &sess.ModelID, &sess.Preset, &sess.State, &sess.CreatedAt, &sess.UpdatedAt)
	if err == sql.ErrNoRows {
		return sess, fmt.Errorf("storage: session %s: %w", id, ErrNotFound)
	}
	return sess, err
}

func (s *sqliteStore) touch(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET updated_at = ? WHERE id = ?`, Now(), id)
	return err
}

func (s *sqliteStore) UpdateSessionState(ctx context.Context, id, state string) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE sessions SET state = ?, updated_at = ? WHERE id = ?`, state, Now(), id); err != nil {
		return err
	}
	return nil
}

func (s *sqliteStore) UpdateSessionTitle(ctx context.Context, id, title string) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE sessions SET title = ?, updated_at = ? WHERE id = ?`, title, Now(), id); err != nil {
		return err
	}
	return nil
}

func (s *sqliteStore) NextSeq(ctx context.Context, sessionID string) (int64, error) {
	var seq sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT MAX(seq) FROM messages WHERE session_id = ?`, sessionID).Scan(&seq)
	if err != nil {
		return 0, err
	}
	return seq.Int64 + 1, nil
}

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
	seq, err := s.NextSeq(ctx, m.SessionID)
	if err != nil {
		return err
	}
	m.Seq = seq
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO messages (id, session_id, seq, role, content, tool_call_id, tool_name, tool_calls_json, turn_id, trust, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.SessionID, m.Seq, m.Role, m.Content, m.ToolCallID, m.ToolName, m.ToolCallsJSON, m.TurnID, m.Trust, m.CreatedAt)
	if err != nil {
		return err
	}
	return s.touch(ctx, m.SessionID)
}

func (s *sqliteStore) Transcript(ctx context.Context, sessionID string) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, seq, role, content, tool_call_id, tool_name, tool_calls_json, turn_id, trust, created_at
		 FROM messages WHERE session_id = ? ORDER BY seq`, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Seq, &m.Role, &m.Content, &m.ToolCallID, &m.ToolName, &m.ToolCallsJSON, &m.TurnID, &m.Trust, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *sqliteStore) AppendToolCall(ctx context.Context, tc *ToolCallRecord) error {
	if tc.ID == "" {
		tc.ID = NewID()
	}
	if tc.CreatedAt == "" {
		tc.CreatedAt = Now()
	}
	if tc.Status == "" {
		tc.Status = "running"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tool_calls (id, session_id, turn_id, name, tool_ref, match_key, risk, reversible, decision, args_json, result_json, exec_hash, status, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		tc.ID, tc.SessionID, tc.TurnID, tc.Name, tc.ToolRef, tc.MatchKey, tc.Risk, tc.Reversible, tc.Decision, tc.ArgsJSON, tc.ResultJSON, tc.ExecHash, tc.Status, tc.CreatedAt)
	return err
}

func (s *sqliteStore) AppendApproval(ctx context.Context, a *Approval) error {
	if a.ID == "" {
		a.ID = NewID()
	}
	if a.CreatedAt == "" {
		a.CreatedAt = Now()
	}
	if a.DecidedAt == "" {
		a.DecidedAt = a.CreatedAt
	}
	if a.DecidedBy == "" {
		a.DecidedBy = "user"
	}
	if a.ContentTrust == "" {
		a.ContentTrust = TrustTrusted
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO approvals (id, session_id, turn_id, tool_call_id, tool_ref, match_key, level, decision, scope, args_json, exec_hash, content_trust, note, decided_by, decided_at, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.SessionID, a.TurnID, a.ToolCallID, a.ToolRef, a.MatchKey, a.Level, a.Decision, a.Scope, a.ArgsJSON, a.ExecHash, a.ContentTrust, a.Note, a.DecidedBy, a.DecidedAt, a.CreatedAt)
	return err
}

func (s *sqliteStore) HasSessionApproval(ctx context.Context, sessionID, matchKey string) (bool, error) {
	if matchKey == "" {
		return false, nil
	}
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM approvals
		 WHERE session_id = ? AND match_key = ? AND scope = 'session' AND decision = 'allow'`,
		sessionID, matchKey).Scan(&n)
	return n > 0, err
}

func (s *sqliteStore) UpdateToolCallResult(ctx context.Context, id, resultJSON, status string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE tool_calls SET result_json = ?, status = ? WHERE id = ?`, resultJSON, status, id)
	return err
}

func (s *sqliteStore) CreatePermissionRule(ctx context.Context, r *PermissionRule) error {
	if r.ID == "" {
		r.ID = NewID()
	}
	if r.CreatedAt == "" {
		r.CreatedAt = Now()
	}
	if r.Decision == "" {
		r.Decision = "allow"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO permission_rules (id, tool_ref, action, match_key, decision, note, created_at, last_used_at, use_count)
		 VALUES (?,?,?,?,?,?,?,?,?)
		 ON CONFLICT (tool_ref, action, match_key) DO UPDATE SET note = excluded.note`,
		r.ID, r.ToolRef, r.Action, r.MatchKey, r.Decision, r.Note, r.CreatedAt, r.LastUsedAt, r.UseCount)
	return err
}

func (s *sqliteStore) MatchPermissionRule(ctx context.Context, toolRef, action, matchKey string) (PermissionRule, bool, error) {
	var r PermissionRule
	err := s.db.QueryRowContext(ctx,
		`SELECT id, tool_ref, action, match_key, decision, note, created_at, last_used_at, use_count
		 FROM permission_rules WHERE tool_ref = ? AND action = ? AND match_key = ? AND decision = 'allow'`,
		toolRef, action, matchKey).
		Scan(&r.ID, &r.ToolRef, &r.Action, &r.MatchKey, &r.Decision, &r.Note, &r.CreatedAt, &r.LastUsedAt, &r.UseCount)
	if err == sql.ErrNoRows {
		return r, false, nil
	}
	if err != nil {
		return r, false, err
	}
	return r, true, nil
}

func (s *sqliteStore) ListPermissionRules(ctx context.Context) ([]PermissionRule, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tool_ref, action, match_key, decision, note, created_at, last_used_at, use_count
		 FROM permission_rules ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []PermissionRule
	for rows.Next() {
		var r PermissionRule
		if err := rows.Scan(&r.ID, &r.ToolRef, &r.Action, &r.MatchKey, &r.Decision, &r.Note, &r.CreatedAt, &r.LastUsedAt, &r.UseCount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *sqliteStore) DeletePermissionRule(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM permission_rules WHERE id = ?`, id)
	return err
}

func (s *sqliteStore) TouchPermissionRule(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE permission_rules SET last_used_at = ?, use_count = use_count + 1 WHERE id = ?`, Now(), id)
	return err
}

func (s *sqliteStore) AppendEvent(ctx context.Context, sessionID, typ, payloadJSON string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO events (session_id, type, payload_json, created_at) VALUES (?,?,?,?)`,
		sessionID, typ, payloadJSON, Now())
	return err
}

// AppendAudit seals one row into the hash chain. The row insert and the head
// update share one transaction; a failed append leaves the chain unchanged.
func (s *sqliteStore) AppendAudit(ctx context.Context, entry audit.Record) error {
	detail, err := audit.NormalizeDetail(entry.Detail)
	if err != nil {
		return err
	}
	entry.Detail = detail
	if entry.CreatedAt == "" {
		entry.CreatedAt = Now()
	}
	s.auditMu.Lock()
	defer s.auditMu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin audit: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var lastID int64
	var lastHash string
	if err := tx.QueryRowContext(ctx,
		`SELECT last_id, last_hash FROM audit_chain WHERE id = 1`).Scan(&lastID, &lastHash); err != nil {
		return fmt.Errorf("storage: read audit head: %w", err)
	}
	entry.PrevHash = lastHash
	entry.EntryHash, err = audit.Hash(lastHash, entry)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO audit_log
		 (session_id, turn_id, trace_id, action, decision, reason, detail_json, created_at, prev_hash, entry_hash)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		entry.SessionID, entry.TurnID, entry.TraceID, entry.Action, entry.Decision,
		entry.Reason, entry.Detail, entry.CreatedAt, entry.PrevHash, entry.EntryHash)
	if err != nil {
		return fmt.Errorf("storage: insert audit: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("storage: audit id: %w", err)
	}
	entry.ID = id
	if _, err := tx.ExecContext(ctx,
		`UPDATE audit_chain SET last_id = ?, last_hash = ?, updated_at = ? WHERE id = 1`,
		id, entry.EntryHash, entry.CreatedAt); err != nil {
		return fmt.Errorf("storage: update audit head: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit audit: %w", err)
	}
	return nil
}

// ListAudit returns rows in ascending ID order. sessionID="" means all rows;
// limit>0 returns the newest N rows (still ascending).
func (s *sqliteStore) ListAudit(ctx context.Context, sessionID string, limit ...int) ([]audit.Record, error) {
	query := `SELECT id, session_id, turn_id, trace_id, action, decision, reason, detail_json, created_at, prev_hash, entry_hash FROM audit_log`
	var args []any
	if sessionID != "" {
		query += ` WHERE session_id = ?`
		args = append(args, sessionID)
	}
	n := 0
	if len(limit) > 0 {
		n = limit[0]
	}
	if n > 0 {
		query = `SELECT * FROM (` + query + ` ORDER BY id DESC LIMIT ?) ORDER BY id ASC`
		args = append(args, n)
	} else {
		query += ` ORDER BY id ASC`
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []audit.Record
	for rows.Next() {
		var a audit.Record
		if err := rows.Scan(&a.ID, &a.SessionID, &a.TurnID, &a.TraceID, &a.Action, &a.Decision,
			&a.Reason, &a.Detail, &a.CreatedAt, &a.PrevHash, &a.EntryHash); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// VerifyAudit recomputes the chain and compares it with the stored head.
func (s *sqliteStore) VerifyAudit(ctx context.Context) (audit.VerifyResult, error) {
	rows, err := s.ListAudit(ctx, "")
	if err != nil {
		return audit.VerifyResult{}, err
	}
	var verifier audit.Verifier
	for _, row := range rows {
		verifier.Add(row)
	}
	var chainID int64
	var chainHash string
	if err := s.db.QueryRowContext(ctx,
		`SELECT last_id, last_hash FROM audit_chain WHERE id = 1`).Scan(&chainID, &chainHash); err != nil {
		return audit.VerifyResult{}, fmt.Errorf("storage: read audit head: %w", err)
	}
	return verifier.Finish(chainID, chainHash), nil
}

func (s *sqliteStore) CreateFileBackup(ctx context.Context, b *rollback.Backup) error {
	if b.ID == "" {
		b.ID = NewID()
	}
	if b.CreatedAt == "" {
		b.CreatedAt = Now()
	}
	_, err := s.db.ExecContext(ctx,
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
}

func (s *sqliteStore) UpdateFileBackupWritten(ctx context.Context, id, writtenSHA, verifiedAt string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE file_backups SET written_sha256 = ?, verified_at = ? WHERE id = ?`,
		writtenSHA, verifiedAt, id)
	return err
}

func (s *sqliteStore) MarkFileBackupRestored(ctx context.Context, id, discardedRel, restoredAt string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE file_backups SET discarded_rel = ?, restored_at = ? WHERE id = ?`,
		discardedRel, restoredAt, id)
	return err
}

func (s *sqliteStore) GetFileBackup(ctx context.Context, id string) (rollback.Backup, error) {
	var b rollback.Backup
	var mode int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, session_id, turn_id, rel_path, kind, reason, version, mode, size,
		        backup_rel, backup_sha256, written_sha256, discarded_rel, restore_of,
		        created_at, verified_at, restored_at
		 FROM file_backups WHERE id = ?`, id).
		Scan(&b.ID, &b.SessionID, &b.TurnID, &b.RelPath, &b.Kind, &b.Reason, &b.Version,
			&mode, &b.Size, &b.BackupRel, &b.BackupSHA256, &b.WrittenSHA256,
			&b.DiscardedRel, &b.RestoreOf, &b.CreatedAt, &b.VerifiedAt, &b.RestoredAt)
	if err == sql.ErrNoRows {
		return b, fmt.Errorf("storage: backup %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return b, err
	}
	b.Mode = uint32(mode)
	return b, nil
}

func (s *sqliteStore) LatestFileBackupVersion(ctx context.Context, sessionID, relPath string) (int64, error) {
	var version int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM file_backups WHERE session_id = ? AND rel_path = ?`,
		sessionID, relPath).Scan(&version)
	return version, err
}

func (s *sqliteStore) ListFileBackups(ctx context.Context, sessionID string) ([]rollback.Backup, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, turn_id, rel_path, kind, reason, version, mode, size,
		        backup_rel, backup_sha256, written_sha256, discarded_rel, restore_of,
		        created_at, verified_at, restored_at
		 FROM file_backups WHERE session_id = ? ORDER BY rel_path, version DESC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []rollback.Backup
	for rows.Next() {
		var b rollback.Backup
		var mode int64
		if err := rows.Scan(&b.ID, &b.SessionID, &b.TurnID, &b.RelPath, &b.Kind, &b.Reason,
			&b.Version, &mode, &b.Size, &b.BackupRel, &b.BackupSHA256, &b.WrittenSHA256,
			&b.DiscardedRel, &b.RestoreOf, &b.CreatedAt, &b.VerifiedAt, &b.RestoredAt); err != nil {
			return nil, err
		}
		b.Mode = uint32(mode)
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *sqliteStore) BackupUsage(ctx context.Context) (int64, error) {
	var total int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(size), 0) FROM file_backups WHERE backup_rel <> '' OR discarded_rel <> ''`).
		Scan(&total)
	return total, err
}

func (s *sqliteStore) DeleteFileBackup(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM file_backups WHERE id = ?`, id)
	return err
}

func (s *sqliteStore) KVGet(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return strings.TrimSpace(v), err
}

func (s *sqliteStore) KVSet(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO kv (key, value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// MarkInterruptedNonTerminal flips sessions that were mid-run when the app
// exited to "interrupted". Idle sessions have no run in flight and stay idle.
func (s *sqliteStore) MarkInterruptedNonTerminal(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET state = 'interrupted', updated_at = ?
		 WHERE state IN ('thinking', 'running_tool', 'persisting', 'awaiting_approval', 'paused')`, Now())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// MarkRunningToolCallsUnknown marks tool calls interrupted mid-flight. The
// result is genuinely unknown: recovery must not claim success or failure.
func (s *sqliteStore) MarkRunningToolCallsUnknown(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE tool_calls SET status = 'unknown' WHERE status = 'running'`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
