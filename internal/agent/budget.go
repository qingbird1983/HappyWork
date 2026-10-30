package agent

import (
	"time"

	"github.com/happywork/happywork/internal/model"
)

// BudgetConfig holds the Phase 2 limits (design.md §10).
type BudgetConfig struct {
	MaxTurns     int
	MaxToolCalls int
	MaxTokens    int64
	MaxWallClock time.Duration
	MaxCostUSD   float64

	// One-time grace applied when the user resumes a budget pause.
	GraceTurns     int
	GraceToolCalls int
	GraceTokens    int64
	GraceWallClock time.Duration
	GraceCostUSD   float64
}

// DefaultBudget returns the shipped defaults.
func DefaultBudget() BudgetConfig {
	return BudgetConfig{
		MaxTurns:       50,
		MaxToolCalls:   100,
		MaxTokens:      5_000_000,
		MaxWallClock:   30 * time.Minute,
		MaxCostUSD:     2.00,
		GraceTurns:     10,
		GraceTokens:    500_000,
		GraceWallClock: 10 * time.Minute,
		GraceCostUSD:   0.50,
	}
}

// Clock is injectable so approval timeouts and wall-clock budgets are testable
// without sleeping.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}

// budgetItem identifies a limit for pause/grace bookkeeping.
type budgetItem string

const (
	budgetTurns     budgetItem = "turns"
	budgetToolCalls budgetItem = "tool_calls"
	budgetTokens    budgetItem = "tokens"
	budgetWallClock budgetItem = "wall_clock"
	budgetCost      budgetItem = "cost"
)

// budgetState is per-session bookkeeping. Turn-related counters reset on each
// Submit; token/cost totals accumulate for the session.
type budgetState struct {
	cfg   BudgetConfig
	clock Clock

	runStart  time.Time
	turns     int
	toolCalls int
	tokens    int64
	cost      float64

	turnLimitAdd  int
	toolLimitAdd  int
	wallLimitAdd  time.Duration
	tokenLimitAdd int64
	costLimitAdd  float64

	gracedTurns     bool
	gracedToolCalls bool
	gracedTokens    bool
	gracedWall      bool
	gracedCost      bool
}

func newBudgetState(cfg BudgetConfig, clock Clock) *budgetState {
	if cfg.MaxTurns <= 0 {
		cfg = DefaultBudget()
	}
	if clock == nil {
		clock = realClock{}
	}
	return &budgetState{cfg: cfg, clock: clock}
}

func (b *budgetState) resetRun() {
	b.runStart = b.clock.Now()
	b.turns = 0
	b.toolCalls = 0
	b.gracedTurns = false
	b.gracedToolCalls = false
	b.gracedWall = false
}

func (b *budgetState) addUsage(u model.Usage, estimated bool, modelName string, promptChars, completionChars int) (turnTokens int, estimatedCost float64) {
	if estimated {
		turnTokens = (promptChars + completionChars) / 4
		if turnTokens < 1 {
			turnTokens = 1
		}
		b.tokens += int64(turnTokens)
		c := estimateCost(modelName, model.Usage{
			PromptTokens:     promptChars / 4,
			CompletionTokens: completionChars / 4,
		})
		b.cost += c
		return turnTokens, c
	}
	turnTokens = u.TotalTokens
	if turnTokens == 0 {
		turnTokens = u.PromptTokens + u.CompletionTokens
	}
	b.tokens += int64(turnTokens)
	c := estimateCost(modelName, u)
	b.cost += c
	return turnTokens, c
}

// estimateCost converts usage into USD using the research/02 peak prices.
func estimateCost(modelName string, u model.Usage) float64 {
	var hitPrice, missPrice, outPrice float64
	switch modelName {
	case "deepseek-flash":
		hitPrice, missPrice, outPrice = 0.006, 0.30, 1.20
	case "deepseek-v4-pro":
		hitPrice, missPrice, outPrice = 0.044, 1.32, 3.96
	default:
		return 0
	}
	hit, miss := u.CacheHitTokens, u.CacheMissTokens
	if hit+miss == 0 {
		miss = u.PromptTokens
	}
	return (float64(hit)*hitPrice + float64(miss)*missPrice + float64(u.CompletionTokens)*outPrice) / 1_000_000
}
