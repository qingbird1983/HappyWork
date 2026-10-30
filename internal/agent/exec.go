package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/happywork/happywork/internal/hook"
	"github.com/happywork/happywork/internal/jcs"
	"github.com/happywork/happywork/internal/model"
	"github.com/happywork/happywork/internal/permission"
	"github.com/happywork/happywork/internal/storage"
	"github.com/happywork/happywork/internal/tool"
	"github.com/happywork/happywork/internal/workspace"
)

// toolResult carries one executed call back into the transcript.
type toolResult struct {
	callID      string
	name        string
	toolRef     string
	trust       string
	argsSummary string
	content     string
}

// execCalls runs each requested tool call in order, applying normalization,
// the permission gate, approvals and the execution lock. Every tool call the
// model requested gets a result, even on cancellation or budget exhaustion, so
// the transcript remains a valid provider history.
func (s *Session) execCalls(ctx context.Context, calls []model.ToolCall, turnID int64) ([]toolResult, error) {
	out := make([]toolResult, 0, len(calls))
	for i, call := range calls {
		if err := s.checkWallClock(ctx); err != nil {
			return append(out, unanswered(calls[i:], "本次运行时长超限，未执行")...), err
		}
		if err := ctx.Err(); err != nil {
			return append(out, unanswered(calls[i:], "操作已取消，结果未知")...), err
		}
		s.budget.toolCalls++
		if s.budget.toolCalls > s.toolLimit() {
			if err := s.pauseForBudget(ctx, budgetToolCalls, "工具调用次数超过上限"); err != nil {
				return append(out, unanswered(calls[i:], "工具调用预算耗尽，未执行")...), err
			}
		}
		result, err := s.execOne(ctx, call, turnID)
		if err != nil {
			note := "操作失败，结果未知"
			if ctx.Err() != nil {
				note = "操作已取消，结果未知"
			}
			return append(out, unanswered(calls[i:], note)...), err
		}
		out = append(out, result)
	}
	return out, nil
}

// unanswered builds placeholder results for calls that never ran.
func unanswered(calls []model.ToolCall, note string) []toolResult {
	out := make([]toolResult, 0, len(calls))
	for _, call := range calls {
		trust := storage.TrustTrusted
		if strings.HasPrefix(call.Name, "mcp__") {
			trust = storage.TrustUntrusted
		}
		out = append(out, toolResult{
			callID:  call.ID,
			name:    call.Name,
			trust:   trust,
			content: storage.MustJSON(map[string]string{"error": note}),
		})
	}
	return out
}

// execOne executes a single call and returns its transcript payload.
func (s *Session) execOne(ctx context.Context, call model.ToolCall, turnID int64) (toolResult, error) {
	sessID := s.info.ID
	recordID := storage.NewID()
	result := toolResult{callID: call.ID, name: call.Name, trust: storage.TrustTrusted, argsSummary: string(call.Arguments)}

	// Step 2: parse arguments. Invalid JSON is a tool error, never a retry.
	var args map[string]any
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		s.auditDeny(ctx, call.Name, "tool arguments are not valid JSON")
		s.emitToolRunning(call, "", "", storage.TrustTrusted, truncateForDisplay(string(call.Arguments)))
		return s.finishCall(ctx, recordID, result, storage.ToolCallRecord{
			TurnID: turnID, Name: call.Name, ArgsJSON: string(call.Arguments), Decision: "deny", Risk: "deny",
		}, storage.MustJSON(map[string]string{"error": "工具参数不是合法 JSON"}), "failed")
	}
	if args == nil {
		args = map[string]any{}
	}

	t, ok := s.tools.Get(call.Name)
	if !ok {
		auditErr := s.mgr.store.AppendAudit(ctx, sessID, call.Name, "deny", "unknown tool")
		if auditErr != nil {
			s.mgr.log.Printf("agent: persist audit: %v", auditErr)
		}
		s.emitToolRunning(call, "", "", storage.TrustTrusted, argsSummary(args))
		return s.finishCall(ctx, recordID, result, storage.ToolCallRecord{
			TurnID: turnID, Name: call.Name, ArgsJSON: string(call.Arguments), Decision: "deny", Risk: "deny",
		}, storage.MustJSON(map[string]string{"error": fmt.Sprintf("unknown tool %q", call.Name)}), "denied")
	}
	meta := t.Meta()
	result.name = meta.Name
	result.toolRef = meta.Ref
	result.trust = meta.Trust
	if result.trust == "" {
		result.trust = storage.TrustTrusted
	}
	// Step 1: before_tool_call hooks. Only whitelisted top-level fields can be
	// patched; the runner rejects everything else and audits each accepted
	// patch by value hash (never by content).
	beforeRes := s.runHooks(ctx, hook.Event{
		Point: hook.BeforeToolCall, SessionID: sessID, TurnID: turnID,
		ToolRef: meta.Ref, ToolName: meta.Name, Action: meta.Action, Trust: result.trust,
		Args: args,
	}, meta.PatchableFields)
	if beforeRes.Denied() {
		s.auditDeny(ctx, meta.Name, "hook: "+beforeRes.DenyReason)
		s.emitToolRunning(call, meta.Ref, result.trust, argsSummary(args), "")
		return s.finishCall(ctx, recordID, result, storage.ToolCallRecord{
			TurnID: turnID, Name: meta.Name, ToolRef: meta.Ref, ArgsJSON: string(call.Arguments),
			Decision: "deny", Risk: "deny",
		}, storage.MustJSON(map[string]string{"error": "Hook 拒绝执行：" + beforeRes.DenyReason}), "denied")
	}
	args = hook.Apply(args, beforeRes.Patches)
	hookWarnings := beforeRes.Warnings
	result.argsSummary = argsSummary(args)
	s.emitToolRunning(call, meta.Ref, result.trust, result.argsSummary, "")

	// Step 3/4: filesystem normalization and sensitive-path hard deny.
	normalizedPath := ""
	sensitive := false
	if !permission.IsMCP(meta.Source) && requiresPath(meta.Ref) {
		rawPath, hasPath := args["path"].(string)
		if !hasPath || strings.TrimSpace(rawPath) == "" {
			if meta.Ref == "builtin://list_dir" {
				rawPath = "."
			} else {
				return s.finishCall(ctx, recordID, result, storage.ToolCallRecord{
					TurnID: turnID, Name: meta.Name, ToolRef: meta.Ref, ArgsJSON: string(call.Arguments), Decision: "deny", Risk: "deny",
				}, storage.MustJSON(map[string]string{"error": "缺少必需参数 path"}), "failed")
			}
		}
		resolved, err := s.root.Resolve(rawPath)
		if err != nil {
			s.auditDeny(ctx, meta.Name, err.Error())
			return s.finishCall(ctx, recordID, result, storage.ToolCallRecord{
				TurnID: turnID, Name: meta.Name, ToolRef: meta.Ref, ArgsJSON: string(call.Arguments), Decision: "deny", Risk: "deny",
			}, storage.MustJSON(map[string]string{"error": err.Error()}), "denied")
		}
		rel, err := s.root.Rel(resolved)
		if err != nil {
			s.auditDeny(ctx, meta.Name, err.Error())
			return s.finishCall(ctx, recordID, result, storage.ToolCallRecord{
				TurnID: turnID, Name: meta.Name, ToolRef: meta.Ref, ArgsJSON: string(call.Arguments), Decision: "deny", Risk: "deny",
			}, storage.MustJSON(map[string]string{"error": err.Error()}), "denied")
		}
		normalizedPath = resolved
		args["path"] = resolved
		if workspace.Sensitive(rel) {
			sensitive = true
		}
	}
	canonicalArgs, err := json.Marshal(args)
	if err != nil {
		return s.finishCall(ctx, recordID, result, storage.ToolCallRecord{
			TurnID: turnID, Name: meta.Name, ToolRef: meta.Ref, ArgsJSON: string(call.Arguments), Decision: "deny", Risk: "deny",
		}, storage.MustJSON(map[string]string{"error": "无法规范化参数"}), "failed")
	}
	canonical := string(canonicalArgs)

	// Step 5: execute lock, permission engine and approval.
	matchKey := ""
	if meta.Ref == "builtin://write_file" && normalizedPath != "" {
		matchKey = permission.WriteMatchKey(filepath.Dir(normalizedPath))
	}
	execHash, hashErr := jcs.Hash([]byte(canonical))
	if hashErr != nil {
		s.auditDeny(ctx, meta.Name, "invalid I-JSON arguments: "+hashErr.Error())
		return s.finishCall(ctx, recordID, result, storage.ToolCallRecord{
			TurnID: turnID, Name: meta.Name, ToolRef: meta.Ref, MatchKey: matchKey,
			ArgsJSON: canonical, Decision: "deny", Risk: "deny",
		}, storage.MustJSON(map[string]string{"error": "工具参数不是合法 I-JSON，已拒绝执行"}), "denied")
	}
	verdict, err := s.mgr.engine.Evaluate(ctx, permission.Request{
		SessionID:       sessID,
		ToolRef:         meta.Ref,
		Name:            meta.Name,
		Source:          meta.Source,
		Trust:           meta.Trust,
		Action:          meta.Action,
		InsideWorkspace: true,
		Sensitive:       sensitive,
		Destructive:     meta.Destructive,
		Reversible:      meta.Reversible,
		MatchKey:        matchKey,
		Path:            normalizedPath,
		Root:            s.root.String(),
		Preset:          s.preset(),
		ContentTrust:    s.contentTrust(),
		Now:             s.clock.Now(),
	})
	if err != nil {
		return toolResult{}, err
	}
	if matchKey == "" {
		matchKey = verdict.MatchKey
	}
	s.emitPermission(meta, verdict)
	if verdict.GrantedBy == permission.ScopePersistent {
		if err := s.mgr.store.AppendAudit(ctx, sessID, "permission_rule_hit", "allow",
			meta.Ref+" "+matchKey); err != nil {
			s.mgr.log.Printf("agent: persist audit: %v", err)
		}
	}
	if verdict.Decision == permission.DecisionDeny {
		s.auditDeny(ctx, meta.Name, verdict.Reason)
		return s.finishCall(ctx, recordID, result, storage.ToolCallRecord{
			TurnID: turnID, Name: meta.Name, ToolRef: meta.Ref, MatchKey: matchKey,
			ArgsJSON: canonical, Decision: "deny", Risk: permission.DecisionDeny, ExecHash: execHash,
		}, storage.MustJSON(map[string]string{"error": "权限拒绝：" + verdict.Reason}), "denied")
	}

	decision := "auto"
	feedback := ""
	if verdict.Decision == permission.DecisionAsk {
		decision, feedback, err = s.askApproval(ctx, call, meta, args, canonical, normalizedPath, matchKey, execHash, verdict, hookWarnings, recordID, turnID)
		if err != nil {
			return toolResult{}, err
		}
		if decision == "deny" || decision == "hook_deny" {
			message := "用户拒绝执行"
			if decision == "hook_deny" {
				message = feedback
			}
			return s.finishCall(ctx, recordID, result, storage.ToolCallRecord{
				TurnID: turnID, Name: meta.Name, ToolRef: meta.Ref, MatchKey: matchKey,
				ArgsJSON: canonical, Decision: "deny", Risk: verdict.Decision, Reversible: meta.Reversible,
				ExecHash: execHash,
			}, storage.MustJSON(map[string]string{"error": message, "feedback": feedback}), "denied")
		}
	}

	// Step 6: record the call before execution.
	record := storage.ToolCallRecord{
		ID: recordID, SessionID: sessID, TurnID: turnID, Name: meta.Name, ToolRef: meta.Ref,
		MatchKey: matchKey, Risk: verdict.Decision, Reversible: meta.Reversible,
		Decision: decision, ArgsJSON: canonical, ExecHash: execHash, Status: "running",
	}
	if err := s.mgr.store.AppendToolCall(ctx, &record); err != nil {
		s.mgr.log.Printf("agent: persist tool call: %v", err)
	}

	// Step 7: re-verify the execution lock and the resolved target immediately
	// before running. Any drift between approval and execution is refused.
	if verify, err := jcs.Hash([]byte(canonical)); err != nil || verify != execHash {
		s.auditDeny(ctx, meta.Name, "execution-lock mismatch")
		return s.finishCall(ctx, recordID, result, record,
			storage.MustJSON(map[string]string{"error": "执行前参数校验失败，已拒绝执行"}), "denied")
	}
	if normalizedPath != "" {
		if err := s.revalidateTarget(normalizedPath); err != nil {
			s.auditDeny(ctx, meta.Name, "execution target changed: "+err.Error())
			return s.finishCall(ctx, recordID, result, record,
				storage.MustJSON(map[string]string{"error": "执行目标在审批后发生变化，已拒绝执行"}), "denied")
		}
	}
	execStart := s.clock.Now()
	value, execErr := t.Exec(ctx, json.RawMessage(canonical))
	status := "ok"
	resultJSON := ""
	if execErr != nil {
		status = "failed"
		if errors.Is(execErr, workspace.ErrOutsideWorkspace) || errors.Is(execErr, workspace.ErrSensitivePath) {
			status = "denied"
			s.auditDeny(ctx, meta.Name, execErr.Error())
		}
		resultJSON = storage.MustJSON(map[string]string{"error": execErr.Error()})
	} else {
		data, err := json.Marshal(value)
		if err != nil {
			status = "failed"
			resultJSON = storage.MustJSON(map[string]string{"error": "结果序列化失败"})
		} else {
			resultJSON = s.capResult(string(data))
		}
	}
	// Step 8: after_tool_call hooks observe the result read-only before the
	// record/message is finalized.
	afterRes := s.runHooks(ctx, hook.Event{
		Point: hook.AfterToolCall, SessionID: sessID, TurnID: turnID,
		ToolRef: meta.Ref, ToolName: meta.Name, Action: meta.Action, Trust: result.trust,
		Args: map[string]any{
			"status":      status,
			"resultBytes": len(resultJSON),
			"durationMs":  s.clock.Now().Sub(execStart).Milliseconds(),
		},
	}, nil)
	s.noticeWarnings(afterRes.Warnings)
	return s.finishCall(ctx, recordID, result, record, resultJSON, status)
}

// askApproval emits the dialog and waits for a decision (or approval timeout →
// paused → Resume). It persists the decided approval row and, for a persistent
// scope, the reusable rule.
func (s *Session) askApproval(ctx context.Context, call model.ToolCall, meta tool.Meta, args map[string]any, canonical, normalizedPath, matchKey, execHash string, verdict permission.Verdict, hookWarnings []string, recordID string, turnID int64) (decision, note string, err error) {
	sessID := s.info.ID
	// before_approval hooks may add warnings, demand a block-level dialog or
	// deny the call outright; they cannot change arguments.
	approvalRes := s.runHooks(ctx, hook.Event{
		Point: hook.BeforeApproval, SessionID: sessID, TurnID: turnID,
		ToolRef: meta.Ref, ToolName: meta.Name, Action: meta.Action, Trust: meta.Trust,
		Args: args, Warnings: hookWarnings,
	}, nil)
	if approvalRes.Denied() {
		s.auditDeny(ctx, meta.Name, "hook: "+approvalRes.DenyReason)
		return "hook_deny", approvalRes.DenyReason, nil
	}
	level := verdict.Level
	if approvalRes.EscalateBlock {
		level = permission.LevelBlock
	}
	warnings := append(append([]string{}, hookWarnings...), approvalRes.Warnings...)
	desc := describeCall(meta, args, normalizedPath, s.root, meta.Source)
	options := []ApprovalOption{{Scope: "once", Label: "仅本次允许", Description: "只执行这一次"}}
	allowSession := verdict.Scope == permission.ScopeSession || verdict.Scope == permission.ScopePersistent
	if allowSession {
		scopeText := "本会话内"
		if meta.Ref == "builtin://write_file" {
			scopeText = "本会话内写该目录下"
		}
		options = append(options, ApprovalOption{
			Scope:       "session",
			Label:       "本会话内允许",
			Description: scopeText + "不再询问",
		})
	}
	allowPersistent := verdict.PersistentAllowed && matchKey != ""
	if allowPersistent {
		options = append(options, ApprovalOption{
			Scope:       "persistent",
			Label:       "持久允许（需输入短语）",
			Description: "本机所有会话有效；可在权限规则中查看和撤销",
		})
	}
	trustWarning := ""
	if s.contentTrust() == permission.Untrusted {
		trustWarning = "当前回合包含外部不可信内容，本次仅允许一次"
	}
	quotaNotice := ""
	if verdict.QuotaExceeded {
		quotaNotice = "自动放行限额已触发，本次需要你确认"
	}
	payload := ApprovalPayload{
		ApprovalID:   storage.NewID(),
		SessionID:    sessID,
		ToolCallID:   call.ID,
		ToolName:     meta.Name,
		ToolRef:      meta.Ref,
		Level:        level,
		Title:        desc.title,
		Summary:      desc.summary,
		Fields:       desc.fields,
		Preview:      desc.preview,
		ScopeOptions: options,
		Source:       meta.Source,
		Reversible:   meta.Reversible,
		ContentTrust: s.contentTrust(),
		TrustWarning: trustWarning,
		QuotaNotice:  quotaNotice,
		Warnings:     warnings,
		Preset:       s.preset(),
	}
	d, err := s.awaitApproval(ctx, payload, canonical, allowSession, allowPersistent)
	if err != nil {
		return "", "", err
	}
	decision = "deny"
	note = d.feedback
	if d.allow {
		decision = "allow"
	}
	now := storage.Now()
	row := storage.Approval{
		ID:           payload.ApprovalID,
		SessionID:    sessID,
		TurnID:       turnID,
		ToolCallID:   recordID,
		ToolRef:      meta.Ref,
		MatchKey:     matchKey,
		Level:        level,
		Decision:     decision,
		Scope:        d.scope,
		ArgsJSON:     canonical,
		ExecHash:     execHash,
		ContentTrust: s.contentTrust(),
		Note:         note,
		DecidedAt:    now,
	}
	if err := s.mgr.store.AppendApproval(ctx, &row); err != nil {
		s.mgr.log.Printf("agent: persist approval: %v", err)
	}
	if d.allow && d.scope == permission.ScopePersistent && matchKey != "" {
		rule := storage.PermissionRule{
			ToolRef: meta.Ref, Action: meta.Action, MatchKey: matchKey,
			Note: "created from approval " + payload.ApprovalID,
		}
		if err := s.mgr.store.CreatePermissionRule(ctx, &rule); err != nil {
			s.mgr.log.Printf("agent: persist permission rule: %v", err)
			s.notice("warn", "持久规则保存失败，本次批准仍然有效")
		} else {
			s.notice("info", "已创建持久写入规则；可在权限规则中查看和撤销")
		}
	}
	if !d.allow {
		s.auditDeny(ctx, meta.Name, "user denied: "+note)
	}
	s.mgr.emitEvent(sessID, EventApprovalResolved, ApprovalResolvedPayload{
		SessionID: sessID, ApprovalID: payload.ApprovalID, Decision: decision, Scope: d.scope,
	})
	if d.allow {
		return decision, "", nil
	}
	return decision, note, nil
}

// revalidateTarget re-resolves a path and rejects sensitive targets or paths
// that changed identity between approval and execution.
func (s *Session) revalidateTarget(path string) error {
	again, err := s.root.Resolve(path)
	if err != nil {
		return err
	}
	if !samePath(again, path) {
		return fmt.Errorf("path changed after approval")
	}
	rel, err := s.root.Rel(again)
	if err != nil {
		return err
	}
	if workspace.Sensitive(rel) {
		return workspace.ErrSensitivePath
	}
	return nil
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if filepath.Separator == '\\' {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// emitPermission records the engine verdict for every tool call.
func (s *Session) emitPermission(meta tool.Meta, v permission.Verdict) {
	s.mgr.emitEvent(s.info.ID, EventPermission, PermissionPayload{
		SessionID:     s.info.ID,
		ToolRef:       meta.Ref,
		Name:          meta.Name,
		Action:        meta.Action,
		Decision:      v.Decision,
		Scope:         v.Scope,
		Reason:        v.Reason,
		Auto:          v.Auto,
		QuotaExceeded: v.QuotaExceeded,
		ContentTrust:  s.contentTrust(),
		GrantedBy:     v.GrantedBy,
		Preset:        s.preset(),
	})
}

// finishCall persists the record outcome, emits the final tool event and
// returns the transcript payload. Persistence uses a background context so a
// cancellation still leaves a coherent record.
func (s *Session) finishCall(_ context.Context, recordID string, result toolResult, record storage.ToolCallRecord, resultJSON, status string) (toolResult, error) {
	bg := context.Background()
	result.content = resultJSON
	if record.ID == "" {
		record.ID = recordID
		record.SessionID = s.info.ID
		if record.Status == "" {
			record.Status = status
		}
		if err := s.mgr.store.AppendToolCall(bg, &record); err != nil {
			s.mgr.log.Printf("agent: persist tool call: %v", err)
		}
	} else if err := s.mgr.store.UpdateToolCallResult(bg, recordID, resultJSON, status); err != nil {
		s.mgr.log.Printf("agent: persist tool result: %v", err)
	}
	s.mgr.emitEvent(s.info.ID, EventTool, ToolPayload{
		SessionID:   s.info.ID,
		ToolCallID:  result.callID,
		Name:        result.name,
		ToolRef:     result.toolRef,
		Trust:       result.trust,
		ArgsSummary: result.argsSummary,
		Status:      status,
		Detail:      truncateDetail(resultJSON),
	})
	return result, nil
}

func truncateDetail(s string) string {
	const limit = 4096
	if len(s) <= limit {
		return s
	}
	return strings.ToValidUTF8(s[:limit], "") + "…"
}

// capResult attaches oversized results instead of inlining them.
func (s *Session) capResult(resultJSON string) string {
	if len(resultJSON) <= tool.MaxInlineResult {
		return resultJSON
	}
	path, digest, err := tool.WriteAttachment(s.mgr.opts.DataDir, s.info.ID, []byte(resultJSON))
	if err != nil {
		s.mgr.log.Printf("agent: attach oversized result: %v", err)
		return storage.MustJSON(map[string]string{"error": "工具结果超过 256 KiB 且附件写入失败"})
	}
	return storage.MustJSON(map[string]any{
		"attachment": map[string]any{"sha256": digest, "path": path, "bytes": len(resultJSON)},
		"note":       "工具结果过大，已写入附件；正文未内联",
	})
}

func (s *Session) emitToolRunning(call model.ToolCall, toolRef, trust, summary, _ string) {
	s.mgr.emitEvent(s.info.ID, EventTool, ToolPayload{
		SessionID: s.info.ID, ToolCallID: call.ID, Name: call.Name,
		ToolRef: toolRef, Trust: trust, ArgsSummary: summary, Status: "running",
	})
}

func (s *Session) auditDeny(ctx context.Context, action, reason string) {
	if err := s.mgr.store.AppendAudit(ctx, s.info.ID, action, "deny", reason); err != nil {
		s.mgr.log.Printf("agent: persist audit: %v", err)
	}
}
