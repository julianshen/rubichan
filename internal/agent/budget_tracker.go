package agent

import (
	"fmt"
	"math"
	"time"
)

const completionThreshold = 0.9

type BudgetAction int

const (
	BudgetContinue BudgetAction = iota
	BudgetStop
)

type BudgetTracker struct {
	ContinuationCount    int
	LastDeltaTokens      int
	LastGlobalTurnTokens int
	StartedAt            time.Time
}

func NewBudgetTracker() *BudgetTracker {
	return &BudgetTracker{
		StartedAt: time.Now(),
	}
}

type CompletionEvent struct {
	ContinuationCount  int
	Pct                int
	TurnTokens         int
	Budget             int
	DiminishingReturns bool
	DurationMs         int64
}

type TokenBudgetDecision struct {
	Action            BudgetAction
	NudgeMessage      string
	ContinuationCount int
	Pct               int
	TurnTokens        int
	Budget            int
	CompletionEvent   *CompletionEvent
}

func CheckTokenBudget(tracker *BudgetTracker, agentID string, budget int, globalTurnTokens int) TokenBudgetDecision {
	if agentID != "" || budget <= 0 {
		return TokenBudgetDecision{Action: BudgetContinue}
	}

	budgetF := float64(budget)
	pct := int(math.Round(float64(globalTurnTokens) / budgetF * 100))
	deltaSinceLastCheck := globalTurnTokens - tracker.LastGlobalTurnTokens

	isDiminishing := tracker.ContinuationCount >= 3 &&
		deltaSinceLastCheck < diminishingThreshold &&
		tracker.LastDeltaTokens < diminishingThreshold

	tracker.LastDeltaTokens = deltaSinceLastCheck
	tracker.LastGlobalTurnTokens = globalTurnTokens

	if !isDiminishing && float64(globalTurnTokens) < budgetF*completionThreshold {
		tracker.ContinuationCount++
		return TokenBudgetDecision{
			Action:            BudgetContinue,
			NudgeMessage:      getBudgetContinuationMessage(pct, globalTurnTokens, budget),
			ContinuationCount: tracker.ContinuationCount,
			Pct:               pct,
			TurnTokens:        globalTurnTokens,
			Budget:            budget,
		}
	}

	if isDiminishing || tracker.ContinuationCount > 0 {
		return TokenBudgetDecision{
			Action: BudgetStop,
			CompletionEvent: &CompletionEvent{
				ContinuationCount:  tracker.ContinuationCount,
				Pct:                pct,
				TurnTokens:         globalTurnTokens,
				Budget:             budget,
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
