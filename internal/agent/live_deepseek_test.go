//go:build manual

// Live DeepSeek acceptance test (A1). Run explicitly:
//
//	go test -tags manual -run TestDeepSeekLive -v ./internal/agent
//
// It reads DEEPSEEK_API_KEY from the environment, never prints it, and is
// excluded from CI (no automated test may call an external service).
package agent

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/happywork/happywork/internal/model/deepseek"
	"github.com/happywork/happywork/internal/storage"
	"github.com/happywork/happywork/internal/workspace"
)

func TestDeepSeekLive(t *testing.T) {
	key := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY"))
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY not set")
	}
	dir := t.TempDir()
	wsDir := filepath.Join(dir, "ws")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "note.md"), []byte("HappyWork 的 Phase 2 目标是受控执行闭环。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := workspace.NewRoot(wsDir)
	if err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(filepath.Join(dir, "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	client, err := deepseek.New(deepseek.Config{
		APIKey:  key,
		BaseURL: os.Getenv("HAPPYWORK_DEEPSEEK_BASE_URL"),
		Model:   os.Getenv("HAPPYWORK_DEEPSEEK_MODEL"),
	})
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	lg := log.New(&logs, "live ", 0)
	em := &recordingEmitter{}
	mgr := NewManager(store, client, em, lg, Options{DataDir: dir, ModelName: client.Model()})
	sess, err := mgr.CreateSession(context.Background(), root.String())
	if err != nil {
		t.Fatal(err)
	}
	s := mgr.sessions[sess.ID]

	if err := s.Send(context.Background(), "请先调用 list_dir 查看工作区，再调用 read_file 读取 note.md，然后用一句话总结文件内容。不要写文件。"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(180 * time.Second)
	for time.Now().Before(deadline) {
		if terminal(s.State()) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if s.State() != StateCompleted {
		t.Fatalf("state = %s (want completed)", s.State())
	}
	msgs, err := store.Transcript(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	tools, assistant := 0, 0
	for _, m := range msgs {
		switch m.Role {
		case storage.RoleTool:
			tools++
			if strings.Contains(m.Content, key) {
				t.Fatal("API key leaked into a tool result")
			}
		case storage.RoleAssistant:
			assistant++
			if strings.Contains(m.Content, key) {
				t.Fatal("API key leaked into assistant text")
			}
		}
	}
	if tools == 0 || assistant < 2 {
		t.Fatalf("expected a tool loop, got tools=%d assistant=%d", tools, assistant)
	}
	if strings.Contains(logs.String(), key) {
		t.Fatal("API key leaked into logs")
	}
	var total int
	var cost float64
	for _, e := range em.all(EventUsage) {
		if p, ok := e.(UsagePayload); ok {
			total = p.TotalTokens
			cost = p.CostUSD
			if p.Estimated {
				t.Fatal("real provider should report usage, not estimates")
			}
		}
	}
	if total <= 0 {
		t.Fatal("no usage reported")
	}
	t.Logf("live deepseek ok: model=%s tools=%d tokens=%d cost=$%.6f", client.Model(), tools, total, cost)
}
