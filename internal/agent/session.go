package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/happywork/happywork/internal/hook"
	"github.com/happywork/happywork/internal/permission"
	"github.com/happywork/happywork/internal/storage"
	"github.com/happywork/happywork/internal/tool"
	"github.com/happywork/happywork/internal/workspace"
)

// Command kinds for the session actor.
const (
	cmdSubmit            = "submit"
	cmdResumeInterrupted = "resume_interrupted"
	cmdDiscard           = "discard"
)

type command struct {
	kind  string
	text  string
	ctx   context.Context
	reply chan error
}

// Session drives one conversation. A single actor goroutine serializes state
// transitions; Cancel is the one direct entry point (design.md §4).
type Session struct {
	mgr   *Manager
	info  storage.Session
	root  workspace.Root
	tools *tool.Registry

	mu                  sync.Mutex
	state               string
	cancel              context.CancelFunc
	running             bool
	starting            bool
	pending             *pendingApproval
	pendingAllowSession bool
	pendingAllowPersist bool
	turnUntrusted       bool

	cmds      chan command
	approvals chan approvalDecision
	resumeCh  chan struct{}

	clock  Clock
	budget *budgetState
}

func newSession(mgr *Manager, info storage.Session, root workspace.Root, reg *tool.Registry) *Session {
	state := info.State
	if state == "" {
		state = StateIdle
	}
	s := &Session{
		mgr:       mgr,
		info:      info,
		root:      root,
		tools:     reg,
		state:     state,
		cmds:      make(chan command, 1),
		approvals: make(chan approvalDecision, 4),
		resumeCh:  make(chan struct{}, 1),
		clock:     mgr.clock,
		budget:    newBudgetState(mgr.budget, mgr.clock),
	}
	go s.loop()
	return s
}

// ID returns the session identifier.
func (s *Session) ID() string { return s.info.ID }

// State returns the current state.
func (s *Session) State() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Info returns a copy of the session metadata.
func (s *Session) Info() storage.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.info
}

func (s *Session) preset() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.info.Preset == "" {
		return "default"
	}
	return s.info.Preset
}

// contentTrust reports whether the current run has seen untrusted tool output.
func (s *Session) contentTrust() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.turnUntrusted {
		return permission.Untrusted
	}
	return permission.Trusted
}

func (s *Session) markUntrusted() {
	s.mu.Lock()
	s.turnUntrusted = true
	s.mu.Unlock()
}

// runHooks executes the configured hooks for one point. A nil runner is a
// no-op; hook failures are already audited by the runner.
func (s *Session) runHooks(ctx context.Context, ev hook.Event, allowedFields []string) hook.Result {
	if s.mgr.hooks == nil {
		return hook.Result{}
	}
	return s.mgr.hooks.Run(ctx, ev, allowedFields)
}

// noticeWarnings surfaces hook warnings to the user without blocking the run.
func (s *Session) noticeWarnings(warnings []string) {
	for _, w := range warnings {
		s.notice("warn", w)
	}
}

// Send submits one user message. It returns after the actor starts the run;
// progress arrives through events. Paused/interrupted sessions must be
// resumed (or discarded) instead.
func (s *Session) Send(ctx context.Context, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("agent: empty message")
	}
	if err := s.enqueueStart(cmdSubmit, ctx, text); err != nil {
		return err
	}
	return nil
}

// Cancel aborts the in-flight run (including approval waits and pauses). It
// does not queue behind the actor: cancellation must be immediate.
func (s *Session) Cancel() {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// enqueueStart claims the session and hands a start command to the actor.
func (s *Session) enqueueStart(kind string, ctx context.Context, text string) error {
	s.mu.Lock()
	if s.running || s.starting {
		s.mu.Unlock()
		return ErrBusy
	}
	if s.state == StatePaused {
		s.mu.Unlock()
		return ErrNotResumable
	}
	if kind == cmdResumeInterrupted {
		if s.state != StateInterrupted {
			s.mu.Unlock()
			return ErrNotResumable
		}
	} else if s.state == StateInterrupted {
		s.mu.Unlock()
		return ErrNotResumable
	}
	s.starting = true
	s.mu.Unlock()

	reply := make(chan error, 1)
	cmd := command{kind: kind, text: text, ctx: ctx, reply: reply}
	select {
	case s.cmds <- cmd:
	case <-ctx.Done():
		s.mu.Lock()
		s.starting = false
		s.mu.Unlock()
		return ctx.Err()
	}
	return <-reply
}

// loop is the actor; it never runs two turns at once.
func (s *Session) loop() {
	for cmd := range s.cmds {
		switch cmd.kind {
		case cmdSubmit, cmdResumeInterrupted:
			runCtx, cancel := context.WithCancel(cmd.ctx)
			s.mu.Lock()
			s.running = true
			s.starting = false
			s.cancel = cancel
			if cmd.kind == cmdSubmit {
				// Content-trust latch is per user request (ADR-0014); a resumed
				// run keeps the latch from the interrupted turn.
				s.turnUntrusted = false
			}
			s.mu.Unlock()
			s.budget.resetRun()
			cmd.reply <- nil

			if cmd.kind == cmdResumeInterrupted {
				s.injectUnknownResults(runCtx)
			}
			s.run(runCtx, cmd.text, cmd.kind == cmdResumeInterrupted)
			cancel()
			s.mu.Lock()
			s.running = false
			s.cancel = nil
			s.mu.Unlock()
		case cmdDiscard:
			s.mu.Lock()
			s.starting = false
			s.mu.Unlock()
			s.setState(StateCancelled)
			s.mgr.emitEvent(s.info.ID, EventDone, DonePayload{SessionID: s.info.ID, FinalState: StateCancelled, Detail: "用户放弃继续"})
			cmd.reply <- nil
		}
	}
}

// setState persists and broadcasts a state transition.
func (s *Session) setState(state string) {
	s.mu.Lock()
	s.state = state
	s.info.State = state
	if terminal(state) {
		// A terminal state ends the run; allow an immediate next Submit even
		// while the actor is still emitting the final events.
		s.running = false
	}
	s.mu.Unlock()
	if err := s.mgr.store.UpdateSessionState(context.Background(), s.info.ID, state); err != nil {
		s.mgr.log.Printf("agent: persist state %s: %v", state, err)
	}
	s.mgr.emitEvent(s.info.ID, EventState, StatePayload{SessionID: s.info.ID, State: state})
}

// run executes the turn loop. resume skips the user-message append (recovery
// continue) but otherwise behaves identically.
func (s *Session) run(ctx context.Context, userText string, resume bool) {
	bg := context.Background()
	sessID := s.info.ID

	if !resume {
		userMsg := storage.Message{ID: storage.NewID(), SessionID: sessID, Role: storage.RoleUser, Content: userText, TurnID: 1}
		if err := s.mgr.store.AppendMessage(bg, &userMsg); err != nil {
			s.mgr.log.Printf("agent: persist user message: %v", err)
		}
		s.mu.Lock()
		title := s.info.Title
		s.mu.Unlock()
		if title == "新会话" {
			_ = s.mgr.store.UpdateSessionTitle(bg, sessID, truncateTitle(userText))
			s.mu.Lock()
			s.info.Title = truncateTitle(userText)
			s.mu.Unlock()
		}
	}
	s.setState(StateThinking)

	for {
		if err := ctx.Err(); err != nil {
			s.finish(StateCancelled, "")
			return
		}
		if err := s.checkWallClock(ctx); err != nil {
			s.finishRunErr(ctx, err)
			return
		}
		if s.budget.turns >= s.turnLimit() {
			if s.budget.gracedTurns {
				s.finish(StateError, ErrBudgetExhausted.Error())
				return
			}
			if err := s.pauseForBudget(ctx, budgetTurns, "轮次上限已到"); err != nil {
				s.finishRunErr(ctx, err)
				return
			}
			continue
		}
		s.budget.turns++
		turnID := int64(s.budget.turns)

		hist, err := s.history(bg)
		if err != nil {
			s.finish(StateError, fmt.Sprintf("load history: %v", err))
			return
		}

		if res := s.runHooks(ctx, hook.Event{
			Point: hook.BeforeModelCall, SessionID: sessID, TurnID: turnID,
			Args: map[string]any{"messages": len(hist)},
		}, nil); res.Denied() {
			s.finish(StateError, "Hook 拒绝本次模型调用："+res.DenyReason)
			return
		}

		assistText, calls, usage, err := s.streamTurn(ctx, hist)
		if err != nil {
			if assistText != "" {
				partial := storage.Message{
					ID: storage.NewID(), SessionID: sessID,
					Role: storage.RoleAssistant, Content: assistText, TurnID: turnID,
				}
				if appendErr := s.mgr.store.AppendMessage(bg, &partial); appendErr != nil {
					s.mgr.log.Printf("agent: persist partial message: %v", appendErr)
				}
			}
			s.finishRunErr(ctx, err)
			return
		}
		afterArgs := map[string]any{
			"textChars": len([]rune(assistText)),
			"toolCalls": len(calls),
		}
		if usage != nil {
			afterArgs["totalTokens"] = usage.TotalTokens
		}
		s.noticeWarnings(s.runHooks(ctx, hook.Event{
			Point: hook.AfterModelCall, SessionID: sessID, TurnID: turnID,
			Args: afterArgs,
		}, nil).Warnings)

		if err := s.recordUsage(ctx, hist, assistText, usage); err != nil {
			s.finishRunErr(ctx, err)
			return
		}

		// A textless assistant turn still requested tools; it must be persisted
		// with its tool_calls so the following tool results have a parent. A
		// cancelled turn drops the tool_calls (they will never run) to keep the
		// provider history valid.
		canceled := ctx.Err() != nil
		if assistText != "" || (len(calls) > 0 && !canceled) {
			am := storage.Message{
				ID: storage.NewID(), SessionID: sessID,
				Role: storage.RoleAssistant, Content: assistText, TurnID: turnID,
			}
			if len(calls) > 0 && !canceled {
				am.ToolCallsJSON = storage.MustJSON(calls)
			}
			if err := s.mgr.store.AppendMessage(bg, &am); err != nil {
				s.mgr.log.Printf("agent: persist assistant message: %v", err)
			}
		}

		if len(calls) == 0 {
			s.finish(StateCompleted, "")
			return
		}
		if canceled {
			s.finish(StateCancelled, "")
			return
		}

		results, execErr := s.execCalls(ctx, calls, turnID)
		s.setState(StatePersisting)
		for _, r := range results {
			tm := storage.Message{
				ID: storage.NewID(), SessionID: sessID,
				Role: storage.RoleTool, Content: r.content,
				ToolCallID: r.callID, ToolName: r.name, TurnID: turnID,
			}
			if r.trust == storage.TrustUntrusted {
				tm.Trust = storage.TrustUntrusted
				s.markUntrusted()
			}
			if err := s.mgr.store.AppendMessage(bg, &tm); err != nil {
				s.mgr.log.Printf("agent: persist tool message: %v", err)
			}
		}
		if execErr != nil {
			s.finishRunErr(ctx, execErr)
			return
		}
		if ctx.Err() != nil {
			s.finish(StateCancelled, "")
			return
		}
		s.setState(StateThinking)
	}
}

func (s *Session) finishRunErr(ctx context.Context, err error) {
	if ctx.Err() != nil {
		s.finish(StateCancelled, "")
		return
	}
	s.finish(StateError, err.Error())
}

func (s *Session) checkWallClock(ctx context.Context) error {
	limit := s.budget.cfg.MaxWallClock + s.budget.wallLimitAdd
	if limit <= 0 {
		return nil
	}
	if s.budget.clock.Now().Sub(s.budget.runStart) <= limit {
		return nil
	}
	if s.budget.gracedWall {
		return ErrBudgetExhausted
	}
	return s.pauseForBudget(ctx, budgetWallClock, "本次运行时长超过上限")
}

// pauseForBudget parks the actor on a budget pause. Resume applies one grace
// unless the item was already graced, in which case the run fails.
func (s *Session) pauseForBudget(ctx context.Context, item budgetItem, detail string) error {
	s.setState(StatePaused)
	s.mgr.emitEvent(s.info.ID, EventPaused, PausedPayload{SessionID: s.info.ID, Reason: "budget", Detail: detail})
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.resumeCh:
			switch item {
			case budgetTurns:
				if s.budget.gracedTurns {
					return ErrBudgetExhausted
				}
				s.budget.gracedTurns = true
				s.budget.turnLimitAdd += s.budget.cfg.GraceTurns
			case budgetWallClock:
				if s.budget.gracedWall {
					return ErrBudgetExhausted
				}
				s.budget.gracedWall = true
				s.budget.wallLimitAdd += s.budget.cfg.GraceWallClock
			case budgetTokens:
				if s.budget.gracedTokens {
					return ErrBudgetExhausted
				}
				s.budget.gracedTokens = true
				s.budget.tokenLimitAdd += s.budget.cfg.GraceTokens
			case budgetCost:
				if s.budget.gracedCost {
					return ErrBudgetExhausted
				}
				s.budget.gracedCost = true
				s.budget.costLimitAdd += s.budget.cfg.GraceCostUSD
			case budgetToolCalls:
				if s.budget.gracedToolCalls {
					return ErrBudgetExhausted
				}
				s.budget.gracedToolCalls = true
				s.budget.toolLimitAdd += s.budget.cfg.GraceToolCalls
			}
			s.setState(StateThinking)
			return nil
		}
	}
}

func (s *Session) turnLimit() int     { return s.budget.cfg.MaxTurns + s.budget.turnLimitAdd }
func (s *Session) toolLimit() int     { return s.budget.cfg.MaxToolCalls + s.budget.toolLimitAdd }
func (s *Session) tokenLimit() int64  { return s.budget.cfg.MaxTokens + s.budget.tokenLimitAdd }
func (s *Session) costLimit() float64 { return s.budget.cfg.MaxCostUSD + s.budget.costLimitAdd }

func (s *Session) notice(level, text string) {
	s.mgr.emitEvent(s.info.ID, EventNotice, NoticePayload{SessionID: s.info.ID, Level: level, Text: text})
}

// finish lands on a terminal state and emits done.
func (s *Session) finish(state, detail string) {
	s.setState(state)
	if detail != "" {
		s.mgr.log.Printf("agent: session %s finished %s: %s", s.info.ID, state, detail)
	}
	if state == StateError && detail != "" {
		s.notice("error", detail)
	}
	s.mgr.emitEvent(s.info.ID, EventDone, DonePayload{SessionID: s.info.ID, FinalState: state, Detail: detail})
}

func requiresPath(ref string) bool {
	switch ref {
	case "builtin://read_file", "builtin://write_file", "builtin://list_dir":
		return true
	}
	return false
}

// truncateForDisplay shortens raw arguments for the UI.
func truncateForDisplay(s string) string {
	if len(s) > 200 {
		return strings.ToValidUTF8(s[:200], "") + "…"
	}
	return s
}

// truncateTitle derives the session title from the first user message.
func truncateTitle(text string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) > 40 {
		return string(runes[:40]) + "…"
	}
	return string(runes)
}
