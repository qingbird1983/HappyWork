package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/happywork/happywork/internal/audit"
	"github.com/happywork/happywork/internal/hook"
	"github.com/happywork/happywork/internal/jcs"
	"github.com/happywork/happywork/internal/model"
	"github.com/happywork/happywork/internal/permission"
	"github.com/happywork/happywork/internal/rollback"
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
		s.auditToolBestEffort(ctx, turnID, call.Name, "", audit.DecisionDeny, "tool arguments are not valid JSON", nil)
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
		s.auditToolBestEffort(ctx, turnID, call.Name, "", audit.DecisionDeny, "unknown tool", nil)
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
		s.auditToolBestEffort(ctx, turnID, meta.Name, meta.Ref, audit.DecisionDeny, "hook: "+beforeRes.DenyReason, nil)
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
				s.auditToolBestEffort(ctx, turnID, meta.Name, meta.Ref, audit.DecisionDeny, "missing path", nil)
				return s.finishCall(ctx, recordID, result, storage.ToolCallRecord{
					TurnID: turnID, Name: meta.Name, ToolRef: meta.Ref, ArgsJSON: string(call.Arguments), Decision: "deny", Risk: "deny",
				}, storage.MustJSON(map[string]string{"error": "缺少必需参数 path"}), "failed")
			}
		}
		resolved, err := s.root.Resolve(rawPath)
		if err != nil {
			s.auditToolBestEffort(ctx, turnID, meta.Name, meta.Ref, audit.DecisionDeny, err.Error(), nil)
			return s.finishCall(ctx, recordID, result, storage.ToolCallRecord{
				TurnID: turnID, Name: meta.Name, ToolRef: meta.Ref, ArgsJSON: string(call.Arguments), Decision: "deny", Risk: "deny",
			}, storage.MustJSON(map[string]string{"error": err.Error()}), "denied")
		}
		rel, err := s.root.Rel(resolved)
		if err != nil {
			s.auditToolBestEffort(ctx, turnID, meta.Name, meta.Ref, audit.DecisionDeny, err.Error(), nil)
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
		s.auditToolBestEffort(ctx, turnID, meta.Name, meta.Ref, audit.DecisionDeny, "cannot normalize arguments", nil)
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
		s.auditToolBestEffort(ctx, turnID, meta.Name, meta.Ref, audit.DecisionDeny,
			"invalid I-JSON arguments: "+hashErr.Error(), map[string]any{"matchKey": matchKey})
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
	auditExtra := map[string]any{
		"matchKey": matchKey, "execHash": execHash, "trust": s.contentTrust(),
		"scope": verdict.Scope, "level": verdict.Level, "quota": verdict.QuotaExceeded,
	}
	if verdict.GrantedBy == permission.ScopePersistent {
		if err := s.auditPermissionRule(ctx, turnID, audit.DecisionHit, matchKey,
			map[string]any{"toolRef": meta.Ref, "matchKey": matchKey}); err != nil {
			s.auditToolBestEffort(ctx, turnID, meta.Name, meta.Ref, audit.DecisionDeny,
				"audit write failed", auditExtra)
			return s.finishCall(ctx, recordID, result, storage.ToolCallRecord{
				TurnID: turnID, Name: meta.Name, ToolRef: meta.Ref, MatchKey: matchKey,
				ArgsJSON: canonical, Decision: "deny", Risk: "deny", ExecHash: execHash,
			}, storage.MustJSON(map[string]string{"error": "审计写入失败，已拒绝执行"}), "denied")
		}
	}
	if verdict.Decision == permission.DecisionDeny {
		s.auditToolBestEffort(ctx, turnID, meta.Name, meta.Ref, audit.DecisionDeny, verdict.Reason, auditExtra)
		return s.finishCall(ctx, recordID, result, storage.ToolCallRecord{
			TurnID: turnID, Name: meta.Name, ToolRef: meta.Ref, MatchKey: matchKey,
			ArgsJSON: canonical, Decision: "deny", Risk: permission.DecisionDeny, ExecHash: execHash,
		}, storage.MustJSON(map[string]string{"error": "权限拒绝：" + verdict.Reason}), "denied")
	}

	decision := "auto"
	feedback := ""
	if verdict.Decision != permission.DecisionAsk {
		decisionLabel := audit.DecisionAllow
		if verdict.Auto {
			decisionLabel = audit.DecisionAuto
		}
		if err := s.auditTool(ctx, turnID, meta.Name, meta.Ref, decisionLabel, verdict.Reason, auditExtra); err != nil {
			return s.finishCall(ctx, recordID, result, storage.ToolCallRecord{
				TurnID: turnID, Name: meta.Name, ToolRef: meta.Ref, MatchKey: matchKey,
				ArgsJSON: canonical, Decision: "deny", Risk: "deny", ExecHash: execHash,
			}, storage.MustJSON(map[string]string{"error": "审计写入失败，已拒绝执行"}), "denied")
		}
	}
	if verdict.Decision == permission.DecisionAsk {
		if err := s.auditTool(ctx, turnID, meta.Name, meta.Ref, audit.DecisionAsk, verdict.Reason, auditExtra); err != nil {
			return s.finishCall(ctx, recordID, result, storage.ToolCallRecord{
				TurnID: turnID, Name: meta.Name, ToolRef: meta.Ref, MatchKey: matchKey,
				ArgsJSON: canonical, Decision: "deny", Risk: "deny", ExecHash: execHash,
			}, storage.MustJSON(map[string]string{"error": "审计写入失败，已拒绝执行"}), "denied")
		}
		decision, feedback, err = s.askApproval(ctx, call, meta, args, canonical, normalizedPath, matchKey, execHash, verdict, hookWarnings, recordID, turnID)
		if err != nil {
			return toolResult{}, err
		}
		if decision == "deny" || decision == "hook_deny" {
			message := "用户拒绝执行"
			if decision == "hook_deny" {
				message = feedback
			}
			s.auditToolBestEffort(ctx, turnID, meta.Name, meta.Ref, audit.DecisionDeny, message,
				map[string]any{"matchKey": matchKey, "execHash": execHash, "scope": "once"})
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
		s.auditToolBestEffort(ctx, turnID, meta.Name, meta.Ref, audit.DecisionDeny,
			"execution-lock mismatch", map[string]any{"execHash": execHash})
		return s.finishCall(ctx, recordID, result, record,
			storage.MustJSON(map[string]string{"error": "执行前参数校验失败，已拒绝执行"}), "denied")
	}
	if normalizedPath != "" {
		if err := s.revalidateTarget(normalizedPath); err != nil {
			s.auditToolBestEffort(ctx, turnID, meta.Name, meta.Ref, audit.DecisionDeny,
				"execution target changed: "+err.Error(), map[string]any{"execHash": execHash})
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
			s.auditToolBestEffort(ctx, turnID, meta.Name, meta.Ref, audit.DecisionDeny,
				execErr.Error(), map[string]any{"execHash": execHash})
		}
		var verifyErr *rollback.VerifyError
		if errors.As(execErr, &verifyErr) {
			s.auditBestEffort(ctx, audit.Record{
				TurnID: turnID, Action: audit.ActionBackup, Decision: audit.DecisionVerifyFail,
				Reason: verifyErr.Error(), Detail: auditDetail(map[string]any{
					"relPath": verifyErr.RelPath, "rolledBack": verifyErr.RolledBack,
				}),
			})
		}
		resultJSON = storage.MustJSON(map[string]string{"error": execErr.Error()})
	} else {
		if wr, ok := value.(tool.WriteResult); ok && wr.BackupID != "" {
			s.auditBackupCreate(ctx, turnID, rollback.WriteOutcome{
				Path: wr.Path, BytesWritten: wr.BytesWritten, Created: wr.Created,
				BackupSaved: wr.BackupSaved, BackupID: wr.BackupID, Version: wr.Version,
				SHA256: wr.SHA256, Verified: wr.Verified, RolledBack: wr.RolledBack,
			})
		}
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
