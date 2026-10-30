package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/happywork/happywork/internal/model"
)

func waitPaused(t *testing.T, em *recordingEmitter) PausedPayload {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if all := em.all(EventPaused); len(all) > 0 {
			p, ok := all[0].(PausedPayload)
			if !ok {
				t.Fatalf("paused payload type %T", all[0])
			}
			return p
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("paused event not emitted")
	return PausedPayload{}
}

func TestBudgetTokenPauseAndGrace(t *testing.T) {
	f := newFixture(t)
	f.mgr.budget.MaxTokens = 1
	f.mgr.budget.GraceTokens = 1_000_000
	s := f.newSession(t)
	f.prov.setScript([]model.Chunk{{Delta: "一段回复"}, {Done: true}})

	if err := s.Send(context.Background(), "你好"); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, StatePaused)
	if p := waitPaused(t, f.emitter); p.Reason != "budget" {
		t.Fatalf("paused = %+v", p)
	}
	if f.emitter.count(EventUsage) != 1 {
		t.Fatalf("usage events = %d", f.emitter.count(EventUsage))
	}
	if err := s.Resume(context.Background()); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	waitTerminal(t, f, s)
	if s.State() != StateCompleted {
		t.Fatalf("state = %s", s.State())
	}
}

func TestBudgetCostPauseAndGrace(t *testing.T) {
	f := newFixture(t)
	f.mgr.opts.ModelName = "deepseek-flash"
	f.mgr.budget.MaxCostUSD = 0.0001
	f.mgr.budget.GraceCostUSD = 1.0
	s := f.newSession(t)
	f.prov.setScript([]model.Chunk{
		{Delta: "费用测试"},
		{Done: true, Usage: &model.Usage{PromptTokens: 1000, CompletionTokens: 1000}},
	})

	if err := s.Send(context.Background(), "你好"); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, StatePaused)
	paused := waitPaused(t, f.emitter)
	if paused.Reason != "budget" || !strings.Contains(paused.Detail, "费用") {
		t.Fatalf("paused = %+v", paused)
	}
	if err := s.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitResumed(t, s)
	waitTerminal(t, f, s)
	if s.State() != StateCompleted {
		t.Fatalf("state = %s", s.State())
	}
}

func TestBudgetToolCallsPause(t *testing.T) {
	f := newFixture(t)
	f.mgr.budget.MaxToolCalls = 1
	if err := os.WriteFile(filepath.Join(f.root.String(), "in.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	call := toolCall("t1", "read_file", map[string]any{"path": "in.txt"})
	call2 := toolCall("t2", "read_file", map[string]any{"path": "in.txt"})
	f.prov.setPhases(writePhases(call, call2)...)
	s := f.newSession(t)

	if err := s.Send(context.Background(), "循环读"); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, StatePaused)
	paused := waitPaused(t, f.emitter)
	if paused.Reason != "budget" || !strings.Contains(paused.Detail, "工具调用") {
		t.Fatalf("paused = %+v", paused)
	}
	s.Cancel()
	waitTerminal(t, f, s)
	if s.State() != StateCancelled {
		t.Fatalf("state = %s", s.State())
	}
}

func TestBudgetWallClockPause(t *testing.T) {
	f := newFixture(t)
	clock := newFakeClock()
	f.mgr.clock = clock
	f.mgr.budget.MaxWallClock = 5 * time.Second
	call := toolCall("wc1", "list_dir", map[string]any{"path": "."})
	f.prov.setPhases(writePhases(call)...)
	f.prov.delay = 50 * time.Millisecond
	s := f.newSession(t)

	if err := s.Send(context.Background(), "看目录"); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, StateThinking)
	clock.Advance(10 * time.Second)
	waitState(t, s, StatePaused)
	paused := waitPaused(t, f.emitter)
	if paused.Reason != "budget" || !strings.Contains(paused.Detail, "时长") {
		t.Fatalf("paused = %+v", paused)
	}
	s.Cancel()
	waitTerminal(t, f, s)
	if s.State() != StateCancelled {
		t.Fatalf("state = %s", s.State())
	}
}
