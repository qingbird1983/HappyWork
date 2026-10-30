package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

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
	if err := s.AppendAudit(ctx, "s1", "read_file", "deny", "outside workspace"); err != nil {
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
	if v, _ := s.KVGet(context.Background(), schemaVersionKey); v != "2" {
		t.Fatalf("schema version = %q, want 2", v)
	}
	old, err := s.GetSession(context.Background(), "old")
	if err != nil {
		t.Fatalf("old session unreadable after upgrade: %v", err)
	}
	if old.ModelID != "" || old.State != "completed" {
		t.Fatalf("old session defaults wrong: %+v", old)
	}
	// The new table must be usable.
	if err := s.AppendApproval(context.Background(), &Approval{SessionID: "old", ToolRef: "x", Level: "confirm", Decision: "allow", Scope: "once"}); err != nil {
		t.Fatalf("approvals table unusable after upgrade: %v", err)
	}
}
