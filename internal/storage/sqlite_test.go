package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/happywork/happywork/internal/audit"
	"github.com/happywork/happywork/internal/rollback"
	"github.com/happywork/happywork/migrations"
)

func openTemp(t *testing.T) Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestMigrateIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "same.db")
	s1, err := Open(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	v1, _ := s1.KVGet(context.Background(), schemaVersionKey)
	_ = s1.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second open (idempotent migration): %v", err)
	}
	defer func() { _ = s2.Close() }()
	v2, _ := s2.KVGet(context.Background(), schemaVersionKey)
	if v1 != v2 {
		t.Fatalf("schema version changed across re-open: %s -> %s", v1, v2)
	}
}

func TestSessionAndTranscriptRoundtrip(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	sess := Session{ID: "s1", WorkspaceRoot: "E:/tmp/ws", Title: "新会话"}
	if err := s.CreateSession(ctx, &sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	msgs := []Message{
		{ID: NewID(), SessionID: "s1", Role: RoleUser, Content: "你好"},
		{ID: NewID(), SessionID: "s1", Role: RoleAssistant, Content: "你好！"},
		{ID: NewID(), SessionID: "s1", Role: RoleTool, Content: `{"ok":true}`, ToolCallID: "c1", ToolName: "read_file"},
	}
	for i := range msgs {
		if err := s.AppendMessage(ctx, &msgs[i]); err != nil {
			t.Fatalf("AppendMessage %d: %v", i, err)
		}
	}

	got, err := s.Transcript(ctx, "s1")
	if err != nil {
		t.Fatalf("Transcript: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("transcript len = %d, want 3", len(got))
	}
	for i := range got {
		if want := int64(i + 1); got[i].Seq != want {
			t.Fatalf("seq[%d] = %d, want %d", i, got[i].Seq, want)
		}
	}
	if got[2].ToolCallID != "c1" || got[2].ToolName != "read_file" {
		t.Fatalf("tool message fields lost: %+v", got[2])
	}

	if _, err := s.GetSession(ctx, "missing"); err == nil {
		t.Fatal("GetSession(missing) should fail")
	}
}

func TestMarkInterruptedNonTerminal(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	for id, state := range map[string]string{
		"a": "thinking",
		"b": "idle",
		"c": "completed",
		"d": "paused",
		"e": "awaiting_approval",
	} {
		sess := Session{ID: id, WorkspaceRoot: "ws", State: state}
		if err := s.CreateSession(ctx, &sess); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.MarkInterruptedNonTerminal(ctx)
	if err != nil {
		t.Fatalf("MarkInterruptedNonTerminal: %v", err)
	}
	if n != 3 {
		t.Fatalf("marked %d, want 3", n)
	}
	for id, want := range map[string]string{"a": "interrupted", "b": "idle", "c": "completed", "d": "interrupted", "e": "interrupted"} {
		sess, err := s.GetSession(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if sess.State != want {
			t.Fatalf("session %s state = %s, want %s", id, sess.State, want)
		}
	}
}

func TestAuditAndEvents(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, &Session{ID: "s1", WorkspaceRoot: "ws"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := s.AppendAudit(ctx, audit.Record{
		SessionID: "s1", Action: "read_file", Decision: "deny",
		Reason: "outside workspace", Detail: `{"toolRef":"builtin://read_file"}`,
	}); err != nil {
		t.Fatalf("AppendAudit: %v", err)
	}
	if err := s.AppendEvent(ctx, "s1", "session:state", `{"state":"idle"}`); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if err := s.AppendToolCall(ctx, &ToolCallRecord{ID: "tc1", SessionID: "s1", Name: "read_file", ArgsJSON: "{}"}); err != nil {
		t.Fatalf("AppendToolCall: %v", err)
	}
	if err := s.UpdateToolCallResult(ctx, "tc1", `{"ok":true}`, "ok"); err != nil {
		t.Fatalf("UpdateToolCallResult: %v", err)
	}
}

func TestPhase2ColumnsRoundtrip(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	sess := Session{ID: "s2", WorkspaceRoot: "ws", ModelID: "deepseek-flash"}
	if err := s.CreateSession(ctx, &sess); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSession(ctx, "s2")
	if err != nil || got.ModelID != "deepseek-flash" {
		t.Fatalf("model id roundtrip: %+v, %v", got, err)
	}
	m := Message{SessionID: "s2", Role: RoleTool, Content: `{"ok":true}`, TurnID: 3, Trust: TrustUntrusted}
	if err := s.AppendMessage(ctx, &m); err != nil {
		t.Fatal(err)
	}
	msgs, err := s.Transcript(ctx, "s2")
	if err != nil || len(msgs) != 1 {
		t.Fatalf("transcript: %+v, %v", msgs, err)
	}
	if msgs[0].TurnID != 3 || msgs[0].Trust != TrustUntrusted {
		t.Fatalf("turn/trust lost: %+v", msgs[0])
	}
	tc := ToolCallRecord{
		SessionID: "s2", TurnID: 3, Name: "write_file", ToolRef: "builtin://write_file",
		MatchKey: "k", Risk: "ask", Reversible: true, Decision: "allow",
		ArgsJSON: `{}`, Status: "running",
	}
	if err := s.AppendToolCall(ctx, &tc); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := s.(*sqliteStore).db.QueryRowContext(ctx,
		`SELECT status FROM tool_calls WHERE id = ?`, tc.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "running" {
		t.Fatalf("status = %s", status)
	}
	if n, err := s.MarkRunningToolCallsUnknown(ctx); err != nil || n != 1 {
		t.Fatalf("MarkRunningToolCallsUnknown = %d, %v", n, err)
	}
	if err := s.(*sqliteStore).db.QueryRowContext(ctx,
		`SELECT status FROM tool_calls WHERE id = ?`, tc.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "unknown" {
		t.Fatalf("status after crash = %s, want unknown", status)
	}
}

func TestPhase3ColumnsRoundtrip(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	sess := Session{ID: "s3", WorkspaceRoot: "ws", Preset: "readonly"}
	if err := s.CreateSession(ctx, &sess); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSession(ctx, "s3")
	if err != nil || got.Preset != "readonly" {
		t.Fatalf("preset roundtrip: %+v, %v", got, err)
	}
	tc := ToolCallRecord{
		ID: "tc3", SessionID: "s3", Name: "write_file", ToolRef: "builtin://write_file",
		ArgsJSON: `{"path":"a.txt"}`, Status: "running", ExecHash: "abc",
	}
	if err := s.AppendToolCall(ctx, &tc); err != nil {
		t.Fatal(err)
	}
	var execHash string
	if err := s.(*sqliteStore).db.QueryRowContext(ctx,
		`SELECT exec_hash FROM tool_calls WHERE id = ?`, tc.ID).Scan(&execHash); err != nil {
		t.Fatal(err)
	}
	if execHash != "abc" {
		t.Fatalf("tool exec hash = %q", execHash)
	}
	if err := s.AppendApproval(ctx, &Approval{
		ID: "a3", SessionID: "s3", ToolRef: "builtin://write_file", Level: "confirm",
		Decision: "allow", Scope: "once", ExecHash: "abc", ContentTrust: TrustUntrusted,
	}); err != nil {
		t.Fatal(err)
	}
	var gotHash, gotTrust string
	if err := s.(*sqliteStore).db.QueryRowContext(ctx,
		`SELECT exec_hash, content_trust FROM approvals WHERE id = 'a3'`).Scan(&gotHash, &gotTrust); err != nil {
		t.Fatal(err)
	}
	if gotHash != "abc" || gotTrust != TrustUntrusted {
		t.Fatalf("approval phase3 columns = %q %q", gotHash, gotTrust)
	}
}

func TestPermissionRules(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	rule := PermissionRule{
		ToolRef: "builtin://write_file", Action: "write", MatchKey: "builtin://write_file|write|ws/docs",
		Note: "docs only",
	}
	if err := s.CreatePermissionRule(ctx, &rule); err != nil {
		t.Fatal(err)
	}
	if rule.ID == "" {
		t.Fatal("rule id not assigned")
	}
	got, ok, err := s.MatchPermissionRule(ctx, "builtin://write_file", "write", "builtin://write_file|write|ws/docs")
	if err != nil || !ok || got.ID != rule.ID || got.Note != "docs only" {
		t.Fatalf("match = %+v, %v, %v", got, ok, err)
	}
	if _, ok, err := s.MatchPermissionRule(ctx, "builtin://write_file", "write", "other"); err != nil || ok {
		t.Fatalf("unexpected match for other key: %v %v", ok, err)
	}
	if err := s.TouchPermissionRule(ctx, rule.ID); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListPermissionRules(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if list[0].UseCount != 1 || list[0].LastUsedAt == "" {
		t.Fatalf("touch not recorded: %+v", list[0])
	}
	// Same key upserts the note instead of duplicating.
	dup := PermissionRule{ToolRef: rule.ToolRef, Action: rule.Action, MatchKey: rule.MatchKey, Note: "updated"}
	if err := s.CreatePermissionRule(ctx, &dup); err != nil {
		t.Fatal(err)
	}
	list, _ = s.ListPermissionRules(ctx)
	if len(list) != 1 || list[0].Note != "updated" || list[0].ID != rule.ID {
		t.Fatalf("upsert = %+v", list)
	}
	if err := s.DeletePermissionRule(ctx, rule.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.ListPermissionRules(ctx); len(list) != 0 {
		t.Fatalf("rule not deleted: %+v", list)
	}
}

func TestMigrationUpgradeFromPhase2(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "phase2.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	stmts, err := migrations.Ordered()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE kv (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := raw.Exec(stmts[i].SQL); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := raw.Exec(`INSERT INTO kv (key, value) VALUES (?, '2')`, schemaVersionKey); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO sessions (id, workspace_root, state, created_at, updated_at)
		VALUES ('p2', 'ws', 'completed', 't', 't')`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO approvals (id, session_id, tool_ref, level, decision, scope, decided_at, created_at)
		VALUES ('old-a', 'p2', 'builtin://write_file', 'confirm', 'allow', 'once', 't', 't')`); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("upgrade open: %v", err)
	}
	defer func() { _ = s.Close() }()
	if v, _ := s.KVGet(context.Background(), schemaVersionKey); v != "4" {
		t.Fatalf("schema version = %q, want 4", v)
	}
	sess, err := s.GetSession(context.Background(), "p2")
	if err != nil || sess.Preset != "default" {
		t.Fatalf("phase2 session upgrade: %+v, %v", sess, err)
	}
	var hash, trust string
	if err := s.(*sqliteStore).db.QueryRowContext(context.Background(),
		`SELECT exec_hash, content_trust FROM approvals WHERE id = 'old-a'`).Scan(&hash, &trust); err != nil {
		t.Fatal(err)
	}
	if hash != "" || trust != "trusted" {
		t.Fatalf("old approval defaults = %q %q", hash, trust)
	}
}

func TestSessionApprovalGrant(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, &Session{ID: "s1", WorkspaceRoot: "ws"}); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.HasSessionApproval(ctx, "s1", "write:docs"); err != nil || ok {
		t.Fatalf("unexpected pre-grant: ok=%v err=%v", ok, err)
	}
	if err := s.AppendApproval(ctx, &Approval{
		ID: "a1", SessionID: "s1", ToolRef: "builtin://write_file", MatchKey: "write:docs",
		Level: "confirm", Decision: "allow", Scope: "session", ArgsJSON: `{}`,
	}); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.HasSessionApproval(ctx, "s1", "write:docs"); err != nil || !ok {
		t.Fatalf("grant not found: ok=%v err=%v", ok, err)
	}
	if ok, _ := s.HasSessionApproval(ctx, "s1", "write:other"); ok {
		t.Fatal("grant leaked to another match key")
	}
}

func TestMigrationUpgradeFromPhase1(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old.db")

	// Build a Phase 1 database: apply only 0001 and record version 1.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	stmts, err := migrations.Ordered()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE kv (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(stmts[0].SQL); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO kv (key, value) VALUES (?, '1')`, schemaVersionKey); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO sessions (id, workspace_root, state, created_at, updated_at)
		VALUES ('old', 'ws', 'completed', 't', 't')`); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("upgrade open: %v", err)
	}
	defer func() { _ = s.Close() }()
	if v, _ := s.KVGet(context.Background(), schemaVersionKey); v != "4" {
		t.Fatalf("schema version = %q, want 4", v)
	}
	old, err := s.GetSession(context.Background(), "old")
	if err != nil {
		t.Fatalf("old session unreadable after upgrade: %v", err)
	}
	if old.ModelID != "" || old.Preset != "default" || old.State != "completed" {
		t.Fatalf("old session defaults wrong: %+v", old)
	}
	// The new table must be usable.
	if err := s.AppendApproval(context.Background(), &Approval{SessionID: "old", ToolRef: "x", Level: "confirm", Decision: "allow", Scope: "once"}); err != nil {
		t.Fatalf("approvals table unusable after upgrade: %v", err)
	}
}

func TestAuditChainAndTriggers(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := s.AppendAudit(ctx, audit.Record{
			SessionID: "s1", TurnID: int64(i + 1), TraceID: "tr",
			Action: audit.ActionToolCall, Decision: audit.DecisionAllow,
			Reason: "allowed", Detail: `{"toolRef":"builtin://read_file"}`,
		}); err != nil {
			t.Fatalf("AppendAudit %d: %v", i, err)
		}
	}
	rows, err := s.ListAudit(ctx, "s1")
	if err != nil || len(rows) != 3 {
		t.Fatalf("ListAudit = %+v, %v", rows, err)
	}
	for i, row := range rows {
		if row.EntryHash == "" || len(row.EntryHash) != 64 {
			t.Fatalf("row %d hash = %q", i, row.EntryHash)
		}
		if i > 0 && row.PrevHash != rows[i-1].EntryHash {
			t.Fatalf("row %d prev does not link", i)
		}
	}
	limited, err := s.ListAudit(ctx, "s1", 2)
	if err != nil || len(limited) != 2 || limited[0].ID != rows[1].ID {
		t.Fatalf("limited list = %+v, %v", limited, err)
	}
	verify, err := s.VerifyAudit(ctx)
	if err != nil || !verify.OK || verify.SealedRows != 3 {
		t.Fatalf("VerifyAudit = %+v, %v", verify, err)
	}
	db := s.(*sqliteStore).db
	if _, err := db.ExecContext(ctx, `UPDATE audit_log SET reason = 'tampered' WHERE id = ?`, rows[0].ID); err == nil {
		t.Fatal("audit UPDATE should be blocked")
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM audit_log WHERE id = ?`, rows[0].ID); err == nil {
		t.Fatal("audit DELETE should be blocked")
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO audit_log (session_id, action, decision, reason, created_at, prev_hash, entry_hash)
		 VALUES ('s1', 'x', 'deny', 'x', 't', 'bad-prev', 'bad-hash')`); err == nil {
		t.Fatal("insert with wrong prev should be blocked")
	}
}

func TestPhase4MigrationLegacyAudit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "phase3.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	stmts, err := migrations.Ordered()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE kv (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := raw.Exec(stmts[i].SQL); err != nil {
			t.Fatalf("apply migration %d: %v", i, err)
		}
	}
	if _, err := raw.Exec(`INSERT INTO kv (key, value) VALUES (?, '3')`, schemaVersionKey); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO sessions (id, workspace_root, state, created_at, updated_at)
		VALUES ('p3', 'ws', 'completed', 't', 't')`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO audit_log (session_id, action, decision, reason, created_at)
		VALUES ('p3', 'legacy_tool', 'deny', 'old row', 't')`); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("upgrade open: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	if v, _ := s.KVGet(ctx, schemaVersionKey); v != "4" {
		t.Fatalf("schema version = %q, want 4", v)
	}
	legacy, err := s.ListAudit(ctx, "p3")
	if err != nil || len(legacy) != 1 || legacy[0].EntryHash != "" {
		t.Fatalf("legacy audit = %+v, %v", legacy, err)
	}
	verify, err := s.VerifyAudit(ctx)
	if err != nil || !verify.OK || verify.LegacyRows != 1 || verify.SealedRows != 0 {
		t.Fatalf("legacy verify = %+v, %v", verify, err)
	}
	if err := s.AppendAudit(ctx, audit.Record{
		SessionID: "p3", Action: audit.ActionToolCall, Decision: audit.DecisionDeny, Reason: "new row",
	}); err != nil {
		t.Fatalf("seal after legacy: %v", err)
	}
	verify, err = s.VerifyAudit(ctx)
	if err != nil || !verify.OK || verify.LegacyRows != 1 || verify.SealedRows != 1 {
		t.Fatalf("post-legacy verify = %+v, %v", verify, err)
	}
	if _, err := s.(*sqliteStore).db.ExecContext(ctx, `UPDATE audit_log SET reason = 'x' WHERE id = ?`, legacy[0].ID); err == nil {
		t.Fatal("legacy audit UPDATE should be blocked too")
	}
}

func TestFileBackupRoundtrip(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.CreateSession(ctx, &Session{ID: "s1", WorkspaceRoot: "ws"}); err != nil {
		t.Fatal(err)
	}
	b := rollback.Backup{
		SessionID: "s1", TurnID: 2, RelPath: "docs/a.md", Kind: rollback.KindOverwrite,
		Reason: rollback.ReasonWrite, Version: 1, Mode: 0o600, Size: 12,
		BackupRel: "s1/docs/a.md/v1.bak", BackupSHA256: "backup-hash", WrittenSHA256: "written-hash",
	}
	if err := s.CreateFileBackup(ctx, &b); err != nil {
		t.Fatalf("CreateFileBackup: %v", err)
	}
	if b.ID == "" {
		t.Fatal("backup id not assigned")
	}
	got, err := s.GetFileBackup(ctx, b.ID)
	if err != nil || got.Mode != 0o600 || got.BackupRel != b.BackupRel || got.Size != 12 {
		t.Fatalf("GetFileBackup = %+v, %v", got, err)
	}
	if v, err := s.LatestFileBackupVersion(ctx, "s1", "docs/a.md"); err != nil || v != 1 {
		t.Fatalf("latest = %d, %v", v, err)
	}
	if usage, err := s.BackupUsage(ctx); err != nil || usage != 12 {
		t.Fatalf("usage = %d, %v", usage, err)
	}
	if err := s.UpdateFileBackupWritten(ctx, b.ID, "new-written", "t1"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetFileBackup(ctx, b.ID)
	if got.WrittenSHA256 != "new-written" || got.VerifiedAt != "t1" {
		t.Fatalf("written update = %+v", got)
	}
	if err := s.MarkFileBackupRestored(ctx, b.ID, "s1/discarded/a.md/x.trash", "t2"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetFileBackup(ctx, b.ID)
	if got.DiscardedRel == "" || got.RestoredAt != "t2" {
		t.Fatalf("restore update = %+v", got)
	}
	create := rollback.Backup{
		SessionID: "s1", RelPath: "new.txt", Kind: rollback.KindCreate,
		Reason: rollback.ReasonWrite, Version: 1, WrittenSHA256: "abc", Size: 3,
	}
	if err := s.CreateFileBackup(ctx, &create); err != nil {
		t.Fatal(err)
	}
	if usage, _ := s.BackupUsage(ctx); usage != 12 {
		t.Fatalf("create record counted in usage: %d", usage)
	}
	list, err := s.ListFileBackups(ctx, "s1")
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if err := s.DeleteFileBackup(ctx, create.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.ListFileBackups(ctx, "s1"); len(list) != 1 {
		t.Fatalf("delete failed: %+v", list)
	}
}
