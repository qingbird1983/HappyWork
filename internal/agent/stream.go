package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/happywork/happywork/internal/model"
)

// history assembles the provider conversation: system prompt plus persisted
// messages (assistant tool_calls restored from tool_calls_json).
func (s *Session) history(ctx context.Context) ([]model.Message, error) {
	msgs, err := s.mgr.store.Transcript(ctx, s.info.ID)
	if err != nil {
		return nil, err
	}
	out := make([]model.Message, 0, len(msgs)+1)
	out = append(out, model.Message{Role: model.RoleSystem, Content: systemPrompt})
	for _, m := range msgs {
		mm := model.Message{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		if m.ToolCallsJSON != "" {
			_ = json.Unmarshal([]byte(m.ToolCallsJSON), &mm.ToolCalls)
		}
		out = append(out, mm)
	}
	return out, nil
}

// streamTurn streams one provider turn. It returns the accumulated text, tool
// calls and provider usage (nil when the mock provider omits it).
func (s *Session) streamTurn(ctx context.Context, hist []model.Message) (string, []model.ToolCall, *model.Usage, error) {
	sessID := s.info.ID
	ch := s.mgr.prov.Stream(ctx, hist, s.tools.Specs())

	var (
		text    strings.Builder
		calls   []model.ToolCall
		usage   *model.Usage
		batcher = newDeltaBatcher(func(joined string) {
			s.mgr.emitEvent(sessID, EventDelta, DeltaPayload{SessionID: sessID, Text: joined})
		})
	)

	for chunk := range ch {
		if err := ctx.Err(); err != nil {
			batcher.flush()
			return text.String(), calls, usage, err
		}
		switch {
		case chunk.Err != nil:
			batcher.flush()
			return text.String(), calls, usage, chunk.Err
		case chunk.Done:
			batcher.flush()
			if chunk.Usage != nil {
				usage = chunk.Usage
			}
			return text.String(), calls, usage, nil
		case chunk.ToolCall != nil:
			batcher.flush()
			calls = append(calls, *chunk.ToolCall)
			s.setState(StateRunningTool)
		default:
			text.WriteString(chunk.Delta)
			batcher.add(chunk.Delta)
		}
	}
	if err := ctx.Err(); err != nil {
		batcher.flush()
		return text.String(), calls, usage, err
	}
	batcher.flush()
	return text.String(), calls, usage, nil
}

func (s *Session) recordUsage(ctx context.Context, hist []model.Message, completion string, usage *model.Usage) error {
	estimated := usage == nil
	promptChars := 0
	for _, m := range hist {
		promptChars += len(m.Content)
	}
	if usage == nil {
		usage = &model.Usage{}
	}
	turnTokens, _ := s.budget.addUsage(*usage, estimated, s.mgr.modelName(), promptChars, len(completion))
	s.mgr.emitEvent(s.info.ID, EventUsage, UsagePayload{
		SessionID:   s.info.ID,
		Model:       s.mgr.modelName(),
		TurnTokens:  turnTokens,
		TotalTokens: int(s.budget.tokens),
		CostUSD:     s.budget.cost,
		Estimated:   estimated,
	})
	// Token/cost limits are session scoped; exceeding them pauses the run.
	if s.budget.tokens > s.tokenLimit() {
		if s.budget.gracedTokens {
			return ErrBudgetExhausted
		}
		if err := s.pauseForBudget(ctx, budgetTokens, fmt.Sprintf("token 用量 %d 超过上限", s.budget.tokens)); err != nil {
			return err
		}
	}
	if s.budget.cost > s.costLimit() {
		if s.budget.gracedCost {
			return ErrBudgetExhausted
		}
		if err := s.pauseForBudget(ctx, budgetCost, fmt.Sprintf("费用 $%.4f 超过上限", s.budget.cost)); err != nil {
			return err
		}
	}
	return nil
}
