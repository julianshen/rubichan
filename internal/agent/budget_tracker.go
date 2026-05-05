package agent

import (
	"fmt"
	"math"
	"time"
)

const completionThreshold = 0.9

// BudgetAction represents the continue/stop decision.
type BudgetAction string

const (
	BudgetContinue BudgetAction = "continue"
	BudgetStop     BudgetAction = "stop"
)

// BudgetTracker tracks token budget state across loop iterations.
type BudgetTracker struct {
	ContinuationCount    int
	LastDeltaTokens      int
	LastGlobalTurnTokens int
	StartedAt            time.Time
}

// NewBudgetTracker creates a new tracker.
func NewBudgetTracker() *BudgetTracker {
	return &BudgetTracker{
		StartedAt: time.Now(),
	}
}

// CompletionEvent holds analytics data when a budget decision completes.
type CompletionEvent struct {
	ContinuationCount  int
	Pct                int
	TurnTokens         int
	Budget             int
	DiminishingReturns bool
	DurationMs         int64
}

// TokenBudgetDecision is the result of CheckTokenBudget.
type TokenBudgetDecision struct {
	Action            BudgetAction
	NudgeMessage      string
	ContinuationCount int
	Pct               int
	TurnTokens        int
	Budget            int
	CompletionEvent   *CompletionEvent
}

// CheckTokenBudget evaluates whether the query loop should continue or stop
// based on the token budget.
func CheckTokenBudget(tracker *BudgetTracker, agentID string, budget *int, globalTurnTokens int) TokenBudgetDecision {
	// Sub-agents and missing/invalid budgets skip budget checking entirely.
	if agentID != "" || budget == nil || *budget <= 0 {
		return TokenBudgetDecision{Action: BudgetContinue}
	}

	turnTokens := globalTurnTokens
	pct := int(math.Round(float64(turnTokens) / float64(*budget) * 100))
	deltaSinceLastCheck := globalTurnTokens - tracker.LastGlobalTurnTokens

	isDiminishing := tracker.ContinuationCount >= 3 &&
		deltaSinceLastCheck < diminishingThreshold &&
		tracker.LastDeltaTokens < diminishingThreshold

	if !isDiminishing && float64(turnTokens) < float64(*budget)*completionThreshold {
		tracker.ContinuationCount++
		tracker.LastDeltaTokens = deltaSinceLastCheck
		tracker.LastGlobalTurnTokens = globalTurnTokens
		return TokenBudgetDecision{
			Action:            BudgetContinue,
			NudgeMessage:      getBudgetContinuationMessage(pct, turnTokens, *budget),
			ContinuationCount: tracker.ContinuationCount,
			Pct:               pct,
			TurnTokens:        turnTokens,
			Budget:            *budget,
		}
	}

	tracker.LastDeltaTokens = deltaSinceLastCheck
	tracker.LastGlobalTurnTokens = globalTurnTokens

	if isDiminishing || tracker.ContinuationCount > 0 {
		return TokenBudgetDecision{
			Action: BudgetStop,
			CompletionEvent: &CompletionEvent{
				ContinuationCount:  tracker.ContinuationCount,
				Pct:                pct,
				TurnTokens:         turnTokens,
				Budget:             *budget,
				DiminishingReturns: isDiminishing,
				DurationMs:         time.Since(tracker.StartedAt).Milliseconds(),
			},
		}
	}

	return TokenBudgetDecision{Action: BudgetStop}
}

func getBudgetContinuationMessage(pct, turnTokens, budget int) string {
	return fmt.Sprintf(
		"You've used %d%% of your token budget (%d/%d tokens). Keep working — you have budget remaining.",
		pct, turnTokens, budget,
	)
}
