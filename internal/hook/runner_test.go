package hook

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type fixtureHook struct {
	name     string
	points   []Point
	priority int
	handle   func(ctx context.Context, ev *Event) (Outcome, error)
}

func (f *fixtureHook) Name() string    { return f.name }
func (f *fixtureHook) Points() []Point { return f.points }
func (f *fixtureHook) Priority() int   { return f.priority }
func (f *fixtureHook) Handle(ctx context.Context, ev *Event) (Outcome, error) {
	if f.handle != nil {
		return f.handle(ctx, ev)
	}
	return Outcome{}, nil
}

type auditLog struct {
	mu      sync.Mutex
	entries []string
}

func (a *auditLog) record(_ Event, hookName, kind, reason string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries = append(a.entries, hookName+"|"+kind+"|"+reason)
}

func (a *auditLog) contains(sub string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, e := range a.entries {
		if strings.Contains(e, sub) {
			return true
		}
	}
	return false
}

func TestRunnerOrderPriorityAndName(t *testing.T) {
	var mu sync.Mutex
	var order []string
	record := func(name string) func(context.Context, *Event) (Outcome, error) {
		return func(context.Context, *Event) (Outcome, error) {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			return Outcome{}, nil
		}
	}
	runner := NewRunner([]Entry{
		{Hook: &fixtureHook{name: "zeta", points: []Point{BeforeToolCall}, handle: record("zeta")}, Priority: 5},
		{Hook: &fixtureHook{name: "alpha", points: []Point{BeforeToolCall}, handle: record("alpha")}, Priority: 5},
		{Hook: &fixtureHook{name: "first", points: []Point{BeforeToolCall}, handle: record("first")}, Priority: 1},
	}, nil)
	runner.Run(context.Background(), Event{Point: BeforeToolCall}, nil)
	want := []string{"first", "alpha", "zeta"}
	if len(order) != len(want) {
		t.Fatalf("order = %v", order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestRunnerPatchWhitelistAndAudit(t *testing.T) {
	audit := &auditLog{}
	patch := func(out Outcome) *fixtureHook {
		return &fixtureHook{name: "patcher", points: []Point{BeforeToolCall}, handle: func(context.Context, *Event) (Outcome, error) {
			return out, nil
		}}
	}
	ev := Event{Point: BeforeToolCall, Args: map[string]any{"path": "a.txt", "content": "old"}}

	okRunner := NewRunner([]Entry{{Hook: patch(Outcome{Patches: []Patch{{Field: "content", Value: "new"}}})}}, audit.record)
	res := okRunner.Run(context.Background(), ev, []string{"content"})
	if res.Denied() || len(res.Patches) != 1 || res.Patches[0].Value != "new" {
		t.Fatalf("res = %+v", res)
	}
	if got := Apply(ev.Args, res.Patches)["content"]; got != "new" {
		t.Fatalf("applied content = %v", got)
	}
	if !audit.contains("field=content") {
		t.Fatalf("patch audit missing: %v", audit.entries)
	}

	audit2 := &auditLog{}
	badRunner := NewRunner([]Entry{{Hook: patch(Outcome{Patches: []Patch{{Field: "path", Value: "b.txt"}}})}}, audit2.record)
	res = badRunner.Run(context.Background(), ev, []string{"content"})
	if len(res.Patches) != 0 || !audit2.contains("rejected") {
		t.Fatalf("non-whitelisted patch accepted: %+v %v", res, audit2.entries)
	}

	audit3 := &auditLog{}
	afterPatch := &fixtureHook{name: "after-patch", points: []Point{AfterToolCall}, handle: func(context.Context, *Event) (Outcome, error) {
		return Outcome{Patches: []Patch{{Field: "content", Value: "x"}}}, nil
	}}
	afterRunner := NewRunner([]Entry{{Hook: afterPatch}}, audit3.record)
	res = afterRunner.Run(context.Background(), Event{Point: AfterToolCall, Args: map[string]any{"content": "old"}}, []string{"content"})
	if len(res.Patches) != 0 || !audit3.contains("before_tool_call") || len(res.Warnings) == 0 {
		t.Fatalf("after patch accepted: %+v %v", res, audit3.entries)
	}
}

func TestRunnerPatchLimits(t *testing.T) {
	patches := make([]Patch, 17)
	for i := range patches {
		patches[i] = Patch{Field: "content", Value: "x"}
	}
	runner := NewRunner([]Entry{{Hook: &fixtureHook{name: "many", points: []Point{BeforeToolCall},
		handle: func(context.Context, *Event) (Outcome, error) { return Outcome{Patches: patches}, nil }}}}, nil)
	res := runner.Run(context.Background(), Event{Point: BeforeToolCall, Args: map[string]any{}}, []string{"content"})
	if len(res.Patches) != 0 {
		t.Fatalf("over-limit patches accepted: %+v", res)
	}
	for _, v := range []any{"ok", float64(1), true, nil} {
		if !scalar(v) {
			t.Fatalf("scalar(%T) = false", v)
		}
	}
	for _, v := range []any{map[string]any{"a": 1}, []any{1}} {
		if scalar(v) {
			t.Fatalf("composite %T accepted as scalar", v)
		}
	}
}

func TestRunnerTimeoutPolicies(t *testing.T) {
	slow := func() *fixtureHook {
		return &fixtureHook{name: "slow", points: []Point{BeforeToolCall},
			handle: func(ctx context.Context, _ *Event) (Outcome, error) {
				time.Sleep(200 * time.Millisecond)
				return Outcome{}, nil
			}}
	}
	start := time.Now()
	warn := NewRunner([]Entry{{Hook: slow(), Timeout: 20 * time.Millisecond, OnError: OnErrorWarn}}, nil)
	res := warn.Run(context.Background(), Event{Point: BeforeToolCall}, nil)
	if res.Denied() || len(res.Warnings) == 0 {
		t.Fatalf("warn policy = %+v", res)
	}
	if time.Since(start) > 150*time.Millisecond {
		t.Fatalf("runner blocked on timeout: %v", time.Since(start))
	}

	deny := NewRunner([]Entry{{Hook: slow(), Timeout: 20 * time.Millisecond, OnError: OnErrorDeny}}, nil)
	if res := deny.Run(context.Background(), Event{Point: BeforeToolCall}, nil); !res.Denied() {
		t.Fatalf("deny policy did not deny: %+v", res)
	}

	audit := &auditLog{}
	closed := NewRunner([]Entry{{Hook: slow(), Timeout: 20 * time.Millisecond, OnError: OnErrorFailClosed}}, audit.record)
	if res := closed.Run(context.Background(), Event{Point: BeforeToolCall}, nil); !res.Denied() {
		t.Fatalf("fail_closed did not deny: %+v", res)
	}
	if !audit.contains(KindFailClosed) {
		t.Fatalf("audit = %v", audit.entries)
	}
}

func TestRunnerPanicRecovery(t *testing.T) {
	boom := &fixtureHook{name: "boom", points: []Point{BeforeToolCall}, handle: func(context.Context, *Event) (Outcome, error) {
		panic("secret value must not reach the audit")
	}}
	audit := &auditLog{}
	runner := NewRunner([]Entry{{Hook: boom, OnError: OnErrorWarn}}, audit.record)
	res := runner.Run(context.Background(), Event{Point: BeforeToolCall}, nil)
	if res.Denied() {
		t.Fatalf("panic denied under warn: %+v", res)
	}
	if audit.contains("secret value") {
		t.Fatalf("panic text leaked into audit: %v", audit.entries)
	}

	closed := NewRunner([]Entry{{Hook: boom, OnError: OnErrorFailClosed}}, audit.record)
	if res := closed.Run(context.Background(), Event{Point: BeforeToolCall}, nil); !res.Denied() {
		t.Fatalf("panic not denied under fail_closed")
	}
}

func TestRunnerAfterPointsReadOnly(t *testing.T) {
	runner := NewRunner([]Entry{{Hook: &fixtureHook{name: "after", points: []Point{AfterToolCall},
		handle: func(context.Context, *Event) (Outcome, error) {
			return Outcome{Patches: []Patch{{Field: "content", Value: "x"}}, DenyReason: "bad", EscalateBlock: true}, nil
		}}}}, nil)
	res := runner.Run(context.Background(), Event{Point: AfterToolCall}, []string{"content"})
	if res.Denied() || len(res.Patches) != 0 || res.EscalateBlock {
		t.Fatalf("after outcome not downgraded: %+v", res)
	}
	if len(res.Warnings) == 0 {
		t.Fatalf("expected a downgraded warning")
	}
}

func TestRunnerEscalateBlockOnlyBeforeApproval(t *testing.T) {
	escalate := &fixtureHook{name: "escalate", points: []Point{BeforeToolCall, BeforeApproval},
		handle: func(context.Context, *Event) (Outcome, error) {
			return Outcome{EscalateBlock: true}, nil
		}}
	runner := NewRunner([]Entry{{Hook: escalate}}, nil)
	if res := runner.Run(context.Background(), Event{Point: BeforeApproval}, nil); !res.EscalateBlock {
		t.Fatalf("before_approval escalation lost")
	}
	if res := runner.Run(context.Background(), Event{Point: BeforeToolCall}, nil); res.EscalateBlock {
		t.Fatalf("before_tool escalation accepted")
	}
}

func TestRunnerErrorsDoNotLeak(t *testing.T) {
	failing := &fixtureHook{name: "fail", points: []Point{BeforeToolCall}, handle: func(context.Context, *Event) (Outcome, error) {
		return Outcome{}, errors.New("token=abcdef")
	}}
	res := NewRunner([]Entry{{Hook: failing}}, nil).Run(context.Background(), Event{Point: BeforeToolCall}, nil)
	if res.Denied() {
		t.Fatalf("warn policy denied: %+v", res)
	}
	for _, w := range res.Warnings {
		if strings.Contains(w, "abcdef") {
			t.Fatalf("warning leaked hook error: %q", w)
		}
	}
}
