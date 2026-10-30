package hook

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	maxPatches  = 16
	maxWarnings = 20
	maxWarning  = 500
)

// Result aggregates the accepted suggestions of one point.
type Result struct {
	Patches       []Patch
	Warnings      []string
	DenyReason    string
	EscalateBlock bool
}

// Denied reports whether any hook demanded a deny.
func (r Result) Denied() bool { return r.DenyReason != "" }

type registration struct {
	name     string
	hook     Hook
	priority int
	timeout  time.Duration
	onError  string
}

// Runner executes the configured hooks for one point.
type Runner struct {
	byPoint map[Point][]registration
	audit   AuditFunc
}

// NewRunner sorts hooks per point by priority then name. onError defaults to
// warn and timeout to DefaultTimeout.
func NewRunner(entries []Entry, audit AuditFunc) *Runner {
	r := &Runner{byPoint: map[Point][]registration{}, audit: audit}
	for _, e := range entries {
		if e.Hook == nil {
			continue
		}
		reg := registration{
			name:     e.Hook.Name(),
			hook:     e.Hook,
			priority: e.Priority,
			timeout:  e.Timeout,
			onError:  e.OnError,
		}
		if reg.timeout <= 0 {
			reg.timeout = DefaultTimeout
		}
		if reg.onError == "" {
			reg.onError = OnErrorWarn
		}
		for _, p := range e.Hook.Points() {
			r.byPoint[p] = append(r.byPoint[p], reg)
		}
	}
	for p := range r.byPoint {
		list := r.byPoint[p]
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].priority != list[j].priority {
				return list[i].priority < list[j].priority
			}
			return list[i].name < list[j].name
		})
	}
	return r
}

// Empty reports whether no hook is registered for any point.
func (r *Runner) Empty() bool { return r == nil || len(r.byPoint) == 0 }

// Run executes every hook at ev.Point in order. allowedFields is the patch
// allowlist of the current tool; it is ignored outside before_tool_call.
func (r *Runner) Run(ctx context.Context, ev Event, allowedFields []string) Result {
	var res Result
	if r == nil {
		return res
	}
	regs := r.byPoint[ev.Point]
	if len(regs) == 0 {
		return res
	}
	patchable := map[string]bool{}
	for _, f := range allowedFields {
		patchable[f] = true
	}
	for _, reg := range regs {
		if err := ctx.Err(); err != nil {
			return res
		}
		out, err, panicked := r.invoke(ctx, reg, ev, res.Warnings)
		if ctx.Err() != nil {
			return res
		}
		if panicked || err != nil {
			reason := "panic"
			if !panicked {
				reason = err.Error()
			}
			r.auditFailure(ev, reg, reason)
			if ev.Point == AfterModelCall || ev.Point == AfterToolCall {
				// Side effects already happened; failures downgrade to warnings.
				res.Warnings = addWarning(res.Warnings, fmt.Sprintf("%s 在 %s 执行失败（%s），已忽略", reg.name, ev.Point, reg.onError))
				continue
			}
			switch reg.onError {
			case OnErrorWarn:
				res.Warnings = addWarning(res.Warnings, fmt.Sprintf("%s 在 %s 执行失败，已忽略", reg.name, ev.Point))
				continue
			case OnErrorDeny:
				res.DenyReason = fmt.Sprintf("Hook %s 拒绝执行", reg.name)
				return res
			default: // fail_closed
				res.DenyReason = fmt.Sprintf("Hook %s 失败，已按 fail_closed 拒绝执行", reg.name)
				return res
			}
		}

		for _, w := range out.Warnings {
			res.Warnings = addWarning(res.Warnings, w)
		}
		if len(out.Patches) > 0 {
			accepted, reason := r.acceptPatches(ev, reg, out.Patches, patchable, len(res.Patches))
			if reason != "" {
				r.auditEvent(ev, reg.name, KindPatch, "rejected: "+reason)
				if ev.Point == AfterModelCall || ev.Point == AfterToolCall {
					res.Warnings = addWarning(res.Warnings, "Hook "+reg.name+" 试图修改参数（已拒绝）")
				} else {
					continue
				}
			} else {
				res.Patches = append(res.Patches, accepted...)
			}
		}
		if out.DenyReason != "" {
			r.auditEvent(ev, reg.name, KindDeny, out.DenyReason)
			if ev.Point == AfterModelCall || ev.Point == AfterToolCall {
				res.Warnings = addWarning(res.Warnings, "Hook "+reg.name+" 报告问题（副作用已发生）："+out.DenyReason)
				continue
			}
			res.DenyReason = out.DenyReason
			return res
		}
		if out.EscalateBlock {
			if ev.Point == BeforeApproval {
				res.EscalateBlock = true
			} else {
				r.auditEvent(ev, reg.name, KindPatch, "rejected: escalate only applies to before_approval")
			}
		}
	}
	return res
}

type hookCall struct {
	out      Outcome
	err      error
	panicked bool
}

// invoke runs one hook under its timeout. A hook that ignores cancellation
// leaks its goroutine until Handle returns; the main loop is never blocked.
func (r *Runner) invoke(ctx context.Context, reg registration, ev Event, warnings []string) (Outcome, error, bool) {
	hctx, cancel := context.WithTimeout(ctx, reg.timeout)
	defer cancel()

	hookEvent := ev
	hookEvent.Args = cloneArgs(ev.Args)
	hookEvent.Warnings = append([]string(nil), warnings...)

	done := make(chan hookCall, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				done <- hookCall{err: fmt.Errorf("panic"), panicked: true}
			}
		}()
		out, err := reg.hook.Handle(hctx, &hookEvent)
		done <- hookCall{out: out, err: err}
	}()

	timer := time.NewTimer(reg.timeout)
	defer timer.Stop()
	select {
	case call := <-done:
		return call.out, call.err, call.panicked
	case <-timer.C:
		return Outcome{}, errors.New("timeout"), false
	case <-ctx.Done():
		return Outcome{}, ctx.Err(), false
	}
}

func (r *Runner) acceptPatches(ev Event, reg registration, patches []Patch, allowed map[string]bool, already int) ([]Patch, string) {
	if ev.Point != BeforeToolCall {
		return nil, "patches are only accepted at before_tool_call"
	}
	if len(patches) > maxPatches || already+len(patches) > maxPatches {
		return nil, fmt.Sprintf("more than %d patches", maxPatches)
	}
	seen := map[string]bool{}
	out := make([]Patch, 0, len(patches))
	for _, p := range patches {
		if !allowed[p.Field] {
			return nil, fmt.Sprintf("field %q is not patchable", p.Field)
		}
		if seen[p.Field] {
			return nil, fmt.Sprintf("duplicate field %q", p.Field)
		}
		seen[p.Field] = true
		if !scalar(p.Value) {
			return nil, fmt.Sprintf("field %q value is not a JSON scalar", p.Field)
		}
		old, _ := ev.Args[p.Field]
		r.auditEvent(ev, reg.name, KindPatch, fmt.Sprintf(
			"field=%s old=%s new=%s", p.Field, hashValue(old), hashValue(p.Value)))
		out = append(out, p)
	}
	return out, ""
}

func (r *Runner) auditFailure(ev Event, reg registration, reason string) {
	kind := KindError
	if reg.onError == OnErrorFailClosed {
		kind = KindFailClosed
	}
	if ev.Point == AfterModelCall || ev.Point == AfterToolCall {
		kind = KindError
	}
	r.auditEvent(ev, reg.name, kind, fmt.Sprintf("point=%s error=%s", ev.Point, reason))
}

func (r *Runner) auditEvent(ev Event, hookName, kind, reason string) {
	if r.audit != nil {
		r.audit(ev, hookName, kind, reason)
	}
}

// Apply returns a copy of args with the accepted patches applied.
func Apply(args map[string]any, patches []Patch) map[string]any {
	out := make(map[string]any, len(args)+len(patches))
	for k, v := range args {
		out[k] = v
	}
	for _, p := range patches {
		out[p.Field] = p.Value
	}
	return out
}

func scalar(v any) bool {
	switch v.(type) {
	case nil, string, bool, float64, float32, int, int64, json.Number:
		return true
	}
	return false
}

func cloneArgs(args map[string]any) map[string]any {
	if args == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		out[k] = v
	}
	return out
}

func addWarning(list []string, text string) []string {
	if len(list) >= maxWarnings {
		return list
	}
	if len(text) > maxWarning {
		text = strings.ToValidUTF8(text[:maxWarning], "") + "…"
	}
	return append(list, text)
}

func hashValue(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "invalid"
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:16]
}
