package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

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
	result.argsSummary = argsSummary(args)
	s.emitToolRunning(call, meta.Ref, result.trust, argsSummary(args), "")

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

	// Step 5: gate and approval.
	matchKey := ""
	allowSession := meta.Ref == "builtin://write_file"
	if allowSession && normalizedPath != "" {
		matchKey = permission.WriteMatchKey(filepath.Dir(normalizedPath))
	}
	verdict := s.mgr.gate.Evaluate(permission.Request{
		ToolRef:         meta.Ref,
		Name:            meta.Name,
		Source:          meta.Source,
		Trust:           meta.Trust,
		Action:          meta.Action,
		InsideWorkspace: true,
		Sensitive:       sensitive,
		Destructive:     meta.Destructive,
		MatchKey:        matchKey,
	})
	if matchKey == "" {
		matchKey = verdict.MatchKey
	}
	if verdict.Decision == permission.DecisionDeny {
		s.auditDeny(ctx, meta.Name, verdict.Reason)
		return s.finishCall(ctx, recordID, result, storage.ToolCallRecord{
			TurnID: turnID, Name: meta.Name, ToolRef: meta.Ref, MatchKey: matchKey,
			ArgsJSON: canonical, Decision: "deny", Risk: permission.DecisionDeny,
		}, storage.MustJSON(map[string]string{"error": "权限拒绝：" + verdict.Reason}), "denied")
	}

	decision := "auto"
	feedback := ""
	if verdict.Decision == permission.DecisionAsk {
		decision, feedback, err = s.askApproval(ctx, call, meta, args, canonical, normalizedPath, matchKey, verdict, allowSession, recordID, turnID)
		if err != nil {
			return toolResult{}, err
		}
		if decision == "deny" {
			return s.finishCall(ctx, recordID, result, storage.ToolCallRecord{
				TurnID: turnID, Name: meta.Name, ToolRef: meta.Ref, MatchKey: matchKey,
				ArgsJSON: canonical, Decision: "deny", Risk: verdict.Decision, Reversible: meta.Reversible,
			}, storage.MustJSON(map[string]string{"error": "用户拒绝执行", "feedback": feedback}), "denied")
		}
	}

	// Step 6: record the call before execution.
	record := storage.ToolCallRecord{
		ID: recordID, SessionID: sessID, TurnID: turnID, Name: meta.Name, ToolRef: meta.Ref,
		MatchKey: matchKey, Risk: verdict.Decision, Reversible: meta.Reversible,
		Decision: decision, ArgsJSON: canonical, Status: "running",
	}
	if err := s.mgr.store.AppendToolCall(ctx, &record); err != nil {
		s.mgr.log.Printf("agent: persist tool call: %v", err)
	}

	// Step 7: execute. The approval snapshot was compared byte-for-byte in
	// awaitApproval, so the bytes passed here are the bytes the user saw.
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
	return s.finishCall(ctx, recordID, result, record, resultJSON, status)
}

// askApproval emits the dialog and waits for a decision (or approval timeout →
// paused → Resume). It persists the decided approval row.
func (s *Session) askApproval(ctx context.Context, call model.ToolCall, meta tool.Meta, args map[string]any, canonical, normalizedPath, matchKey string, verdict permission.Verdict, allowSession bool, recordID string, turnID int64) (decision, note string, err error) {
	sessID := s.info.ID
	// Session grants are only for built-in write approvals.
	if allowSession && matchKey != "" {
		ok, gerr := s.mgr.store.HasSessionApproval(ctx, sessID, matchKey)
		if gerr != nil {
			s.mgr.log.Printf("agent: session approval lookup: %v", gerr)
		}
		if ok {
			s.notice("info", "已匹配本会话的写入授权")
			return "auto", "", nil
		}
	}

	desc := describeCall(meta, args, normalizedPath, s.root, meta.Source)
	options := []ApprovalOption{{Scope: "once", Label: "仅本次允许", Description: "只执行这一次"}}
	if allowSession {
		options = append(options, ApprovalOption{
			Scope:       "session",
			Label:       "本会话内允许",
			Description: "本会话内写该目录下不再询问",
		})
	}
	payload := ApprovalPayload{
		ApprovalID:   storage.NewID(),
		SessionID:    sessID,
		ToolCallID:   call.ID,
		ToolName:     meta.Name,
		ToolRef:      meta.Ref,
		Level:        verdict.Level,
		Title:        desc.title,
		Summary:      desc.summary,
		Fields:       desc.fields,
		Preview:      desc.preview,
		ScopeOptions: options,
		Source:       meta.Source,
		Reversible:   meta.Reversible,
	}
	d, err := s.awaitApproval(ctx, payload, canonical, allowSession)
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
		ID:         payload.ApprovalID,
		SessionID:  sessID,
		TurnID:     turnID,
		ToolCallID: recordID,
		ToolRef:    meta.Ref,
		MatchKey:   matchKey,
		Level:      verdict.Level,
		Decision:   decision,
		Scope:      d.scope,
		ArgsJSON:   canonical,
		Note:       note,
		DecidedAt:  now,
	}
	if err := s.mgr.store.AppendApproval(ctx, &row); err != nil {
		s.mgr.log.Printf("agent: persist approval: %v", err)
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
