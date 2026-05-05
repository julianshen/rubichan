package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckTokenBudgetNilBudget(t *testing.T) {
	tracker := NewBudgetTracker()
	dec := CheckTokenBudget(tracker, "", nil, 100)
	require.Equal(t, BudgetContinue, dec.Action)
}

func TestCheckTokenBudgetZeroBudget(t *testing.T) {
	tracker := NewBudgetTracker()
	budget := 0
	dec := CheckTokenBudget(tracker, "", &budget, 100)
	require.Equal(t, BudgetContinue, dec.Action)
}

func TestCheckTokenBudgetSubAgent(t *testing.T) {
	tracker := NewBudgetTracker()
	budget := 1000
	dec := CheckTokenBudget(tracker, "sub-1", &budget, 100)
	require.Equal(t, BudgetContinue, dec.Action)
}

func TestCheckTokenBudgetContinue(t *testing.T) {
	tracker := NewBudgetTracker()
	budget := 10000
	dec := CheckTokenBudget(tracker, "", &budget, 1000)
	require.Equal(t, BudgetContinue, dec.Action)
	require.NotEmpty(t, dec.NudgeMessage)
	require.Equal(t, 1, dec.ContinuationCount)
	require.Equal(t, 10, dec.Pct)
}

func TestCheckTokenBudgetStopAtThreshold(t *testing.T) {
	tracker := NewBudgetTracker()
	budget := 1000
	// First call at 50% -> continue and set state
	CheckTokenBudget(tracker, "", &budget, 500)
	// Second call at 95% -> stop (over threshold)
	dec := CheckTokenBudget(tracker, "", &budget, 950)
	require.Equal(t, BudgetStop, dec.Action)
	require.NotNil(t, dec.CompletionEvent)
	require.Equal(t, 95, dec.CompletionEvent.Pct)
}

func TestCheckTokenBudgetDiminishingReturns(t *testing.T) {
	tracker := NewBudgetTracker()
	budget := 10000

	// 3 continuations with low deltas (<500 each)
	CheckTokenBudget(tracker, "", &budget, 100) // delta=100
	CheckTokenBudget(tracker, "", &budget, 200) // delta=100
	CheckTokenBudget(tracker, "", &budget, 300) // delta=100

	// 4th with low delta triggers diminishing returns
	dec := CheckTokenBudget(tracker, "", &budget, 350) // delta=50
	require.Equal(t, BudgetStop, dec.Action)
	require.NotNil(t, dec.CompletionEvent)
	require.True(t, dec.CompletionEvent.DiminishingReturns)
}

func TestCheckTokenBudgetResetOnHighDelta(t *testing.T) {
	tracker := NewBudgetTracker()
	budget := 10000

	CheckTokenBudget(tracker, "", &budget, 100) // delta=100
	CheckTokenBudget(tracker, "", &budget, 200) // delta=100
	CheckTokenBudget(tracker, "", &budget, 300) // delta=100

	// High delta resets continuation count
	dec := CheckTokenBudget(tracker, "", &budget, 1000) // delta=700
	require.Equal(t, BudgetContinue, dec.Action)
	require.Equal(t, 4, dec.ContinuationCount)
}

func TestGetBudgetContinuationMessage(t *testing.T) {
	msg := getBudgetContinuationMessage(50, 500, 1000)
	require.Contains(t, msg, "50%")
	require.Contains(t, msg, "500/1000")
}
