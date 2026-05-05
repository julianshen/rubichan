package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckTokenBudgetZeroBudget(t *testing.T) {
	tracker := NewBudgetTracker()
	dec := CheckTokenBudget(tracker, "", 0, 100)
	require.Equal(t, BudgetContinue, dec.Action)
}

func TestCheckTokenBudgetSubAgent(t *testing.T) {
	tracker := NewBudgetTracker()
	dec := CheckTokenBudget(tracker, "sub-1", 1000, 100)
	require.Equal(t, BudgetContinue, dec.Action)
}

func TestCheckTokenBudgetContinue(t *testing.T) {
	tracker := NewBudgetTracker()
	dec := CheckTokenBudget(tracker, "", 10000, 1000)
	require.Equal(t, BudgetContinue, dec.Action)
	require.NotEmpty(t, dec.NudgeMessage)
	require.Equal(t, 1, dec.ContinuationCount)
	require.Equal(t, 10, dec.Pct)
}

func TestCheckTokenBudgetStopAtThreshold(t *testing.T) {
	tracker := NewBudgetTracker()
	// First call at 50% -> continue and set state
	CheckTokenBudget(tracker, "", 1000, 500)
	// Second call at 95% -> stop (over threshold)
	dec := CheckTokenBudget(tracker, "", 1000, 950)
	require.Equal(t, BudgetStop, dec.Action)
	require.NotNil(t, dec.CompletionEvent)
	require.Equal(t, 95, dec.CompletionEvent.Pct)
}

func TestCheckTokenBudgetDiminishingReturns(t *testing.T) {
	tracker := NewBudgetTracker()

	// 3 continuations with low deltas (<500 each)
	CheckTokenBudget(tracker, "", 10000, 100) // delta=100
	CheckTokenBudget(tracker, "", 10000, 200) // delta=100
	CheckTokenBudget(tracker, "", 10000, 300) // delta=100

	// 4th with low delta triggers diminishing returns
	dec := CheckTokenBudget(tracker, "", 10000, 350) // delta=50
	require.Equal(t, BudgetStop, dec.Action)
	require.NotNil(t, dec.CompletionEvent)
	require.True(t, dec.CompletionEvent.DiminishingReturns)
}

func TestCheckTokenBudgetResetOnHighDelta(t *testing.T) {
	tracker := NewBudgetTracker()

	CheckTokenBudget(tracker, "", 10000, 100) // delta=100
	CheckTokenBudget(tracker, "", 10000, 200) // delta=100
	CheckTokenBudget(tracker, "", 10000, 300) // delta=100

	// High delta resets continuation count
	dec := CheckTokenBudget(tracker, "", 10000, 1000) // delta=700
	require.Equal(t, BudgetContinue, dec.Action)
	require.Equal(t, 4, dec.ContinuationCount)
}

func TestGetBudgetContinuationMessage(t *testing.T) {
	msg := getBudgetContinuationMessage(50, 500, 1000)
	require.Contains(t, msg, "50%")
	require.Contains(t, msg, "500/1000")
}
