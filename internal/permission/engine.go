package permission

import (
	"context"
	"fmt"
	"sort"
)

// Engine applies the mapping table, presets, persistent rules, conditions,
// reversibility and DIM4 quotas to a normalized request.
type Engine struct {
	presets PresetSet
	rules   RuleStore
	quota   *quotaTracker
}

// NewEngine builds an engine. A nil preset set uses the builtins; a nil rule
// store disables session/persistent grants (useful in tests and read-only modes).
func NewEngine(presets PresetSet, rules RuleStore) *Engine {
	if presets == nil {
		presets = BuiltinPresets()
	}
	return &Engine{presets: presets, rules: rules, quota: newQuotaTracker()}
}

// PresetNames returns the catalog names sorted for the UI.
func (e *Engine) PresetNames() []string {
	names := make([]string, 0, len(e.presets))
	for name := range e.presets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Presets returns the catalog sorted by name for the UI.
func (e *Engine) Presets() []Preset {
	out := make([]Preset, 0, len(e.presets))
	for _, p := range e.presets {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// HasPreset reports whether a preset is loaded.
func (e *Engine) HasPreset(name string) bool {
	_, ok := e.presets[name]
	return ok
}

// Evaluate returns the unique verdict for one call.
func (e *Engine) Evaluate(ctx context.Context, req Request) (Verdict, error) {
	if req.Sensitive {
		return denied("sensitive path"), nil
	}
	if req.ToolRef == "" {
		return denied("unknown tool"), nil
	}
	row, ok := rowFor(req)
	if !ok {
		return denied("unknown tool row"), nil
	}
	if !IsMCP(req.Source) && !req.InsideWorkspace {
		return denied("outside workspace"), nil
	}
	if req.Preset == "" {
		req.Preset = "default"
	}
	preset, ok := e.presets[req.Preset]
	if !ok {
		return denied("unknown preset"), nil
	}
	row = applyPresets(row, preset)
	if row.Decision == DecisionAllow {
		row.Scope = ScopeNone
		row.Level = LevelInfo
	}
	if req.MatchKey == "" && IsMCP(req.Source) {
		req.MatchKey = MCPMatchKey(req.ToolRef)
	}

	untrustedSideEffect := sideEffect(req.Action) && req.ContentTrust == Untrusted
	if untrustedSideEffect {
		if row.Decision == DecisionAllow {
			row.Decision = DecisionAsk
		}
		row.Scope = ScopeOnce
		if levelRank(row.Level) < levelRank(LevelConfirm) {
			row.Level = LevelConfirm
		}
		row.Persistent = false
	}
	if row.Decision == DecisionDeny {
		return denied(row.Reason), nil
	}

	verdict := Verdict{
		Decision: row.Decision,
		Level:    row.Level,
		Scope:    row.Scope,
		MatchKey: req.MatchKey,
		Reason:   row.Reason,
		Auto:     row.Decision == DecisionAllow,
		PersistentAllowed: row.Persistent && row.Decision == DecisionAsk &&
			scopeRank(row.Scope) >= scopeRank(ScopeSession) && !untrustedSideEffect && row.Reversible,
	}
	if verdict.Decision == DecisionAllow {
		verdict.Level = LevelInfo
	}

	// Persistent and session grants only apply to trusted, side-effect-free
	// approval paths; content-trust demotion handled above removes both.
	if verdict.Decision == DecisionAsk && e.rules != nil && req.MatchKey != "" && !untrustedSideEffect {
		if row.Persistent {
			rule, found, err := e.rules.MatchRule(ctx, req.ToolRef, req.Action, req.MatchKey)
			if err != nil {
				return Verdict{}, err
			}
			if found {
				verdict = granted(verdict, ScopePersistent, "persistent rule")
				if err := e.rules.TouchRule(ctx, rule.ID); err != nil {
					return Verdict{}, err
				}
			}
		}
		if verdict.Decision == DecisionAsk && req.SessionID != "" && scopeRank(row.Scope) >= scopeRank(ScopeSession) {
			ok, err := e.rules.HasSessionGrant(ctx, req.SessionID, req.MatchKey)
			if err != nil {
				return Verdict{}, err
			}
			if ok {
				verdict = granted(verdict, ScopeSession, "session grant")
			}
		}
	}

	if !conditionsSatisfied(row.Conditions, req) {
		switch verdict.Decision {
		case DecisionAllow:
			verdict.Decision = DecisionAsk
			verdict.Level = LevelConfirm
			verdict.Scope = ScopeOnce
			verdict.Auto = false
			verdict.PersistentAllowed = false
		case DecisionAsk:
			return denied("condition not satisfied"), nil
		}
		verdict.Reason += "; condition not satisfied"
	}

	if !row.Reversible {
		switch verdict.Decision {
		case DecisionAllow:
			verdict.Decision = DecisionAsk
			verdict.Auto = false
			verdict.Scope = ScopeOnce
		}
		verdict.Level = LevelBlock
		verdict.PersistentAllowed = false
		verdict.Reason += "; irreversible"
	}

	if verdict.Decision == DecisionAllow && verdict.Auto && row.Quota != nil {
		if !e.quota.allow(quotaKey(req), *row.Quota, req.Now) {
			onExceed := row.Quota.OnExceed
			if onExceed != DecisionDeny {
				onExceed = DecisionAsk
			}
			verdict = Verdict{
				Decision:      onExceed,
				Level:         LevelConfirm,
				Scope:         ScopeOnce,
				MatchKey:      req.MatchKey,
				Reason:        fmt.Sprintf("quota exceeded (%d per %s)", row.Quota.Limit, row.Quota.Window),
				QuotaExceeded: true,
			}
			if onExceed == DecisionDeny {
				verdict.Level = ""
				verdict.Scope = ""
			}
		}
	}
	return verdict, nil
}

func denied(reason string) Verdict {
	return Verdict{Decision: DecisionDeny, Reason: reason}
}

func granted(v Verdict, scope, reason string) Verdict {
	v.Decision = DecisionAllow
	v.Level = LevelInfo
	v.Scope = scope
	v.Reason = reason
	v.Auto = true
	v.GrantedBy = scope
	v.PersistentAllowed = false
	return v
}

func conditionsSatisfied(conds []Condition, req Request) bool {
	for _, cond := range conds {
		if !cond.Satisfied(req) {
			return false
		}
	}
	return true
}
