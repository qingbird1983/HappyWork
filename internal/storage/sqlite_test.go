package storage

import (
	"context"
	"path/filepath"
	"testing"
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
		"d": "error",
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
	if n != 2 {
		t.Fatalf("marked %d, want 2", n)
	}
	for id, want := range map[string]string{"a": "interrupted", "b": "interrupted", "c": "completed"} {
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
