# Token Budget Tracker

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port ccgo's `query/token_budget.go` to rubichan. A `BudgetTracker` tracks token budget state across loop iterations, detecting diminishing returns and injecting nudge messages.

**Architecture:** A `BudgetTracker` struct holds continuation count, last delta tokens, and last global turn tokens. `CheckTokenBudget` evaluates whether to continue or stop based on completion threshold (90%) and diminishing returns (4+ consecutive turns with <500 output tokens delta). The existing `checkDiminishingReturns` in `loopstate.go` is replaced by the full `BudgetTracker`.

**Tech Stack:** Go, standard library (`fmt`, `math`, `time`), existing `loopState` in `internal/agent/loopstate.go`.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/agent/budget_tracker.go` | `BudgetTracker`, `TokenBudgetDecision`, `CompletionEvent`, `CheckTokenBudget` |
| `internal/agent/budget_tracker_test.go` | Tests for continue/stop/diminishing returns |
| `internal/agent/loopstate.go` | Replace `checkDiminishingReturns` with `BudgetTracker` integration |
| `internal/agent/agent.go:1400-1994` | Call `CheckTokenBudget` in `runLoop` |

---

## Chunk 1: Budget Tracker Types

### Task 1: Define BudgetTracker, TokenBudgetDecision, CompletionEvent

**Files:**
- Create: `internal/agent/budget_tracker.go`

**Code:**

```go
package agent

import (
	"fmt"
	"math"
	"time"
)

const (
	completionThreshold  = 0.9
	diminishingThreshold = 500
)

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
```

**Test:**

```go
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
	dec := CheckTokenBudget(tracker, "", &budget, 950)
	require.Equal(t, BudgetStop, dec.Action)
	require.NotNil(t, dec.CompletionEvent)
	require.Equal(t, 95, dec.CompletionEvent.Pct)
}

func TestCheckTokenBudgetDiminishingReturns(t *testing.T) {
	tracker := NewBudgetTracker()
	budget := 10000

	// 3 continuations with low deltas
	CheckTokenBudget(tracker, "", &budget, 100)
	CheckTokenBudget(tracker, "", &budget, 200)
	CheckTokenBudget(tracker, "", &budget, 300)

	// 4th with low delta triggers diminishing returns
	dec := CheckTokenBudget(tracker, "", &budget, 350)
	require.Equal(t, BudgetStop, dec.Action)
	require.NotNil(t, dec.CompletionEvent)
	require.True(t, dec.CompletionEvent.DiminishingReturns)
}

func TestCheckTokenBudgetResetOnHighDelta(t *testing.T) {
	tracker := NewBudgetTracker()
	budget := 10000

	CheckTokenBudget(tracker, "", &budget, 100)
	CheckTokenBudget(tracker, "", &budget, 200)

	// High delta resets continuation count
	dec := CheckTokenBudget(tracker, "", &budget, 1000)
	require.Equal(t, BudgetContinue, dec.Action)
	require.Equal(t, 1, dec.ContinuationCount)
}

func TestGetBudgetContinuationMessage(t *testing.T) {
	msg := getBudgetContinuationMessage(50, 500, 1000)
	require.Contains(t, msg, "50%")
	require.Contains(t, msg, "500/1000")
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestCheckTokenBudget -v
go test ./internal/agent/... -run TestGetBudgetContinuationMessage -v
```

**Expected:** PASS.

- [ ] **Step 1: Write the failing test**
- [ ] **Step 2: Run test to verify it fails**
- [ ] **Step 3: Write minimal implementation**
- [ ] **Step 4: Run test to verify it passes**
- [ ] **Step 5: Commit**

```bash
git add internal/agent/budget_tracker.go internal/agent/budget_tracker_test.go
git commit -m "[STRUCTURAL] Add BudgetTracker with CheckTokenBudget and CompletionEvent"
```

---

## Chunk 2: LoopState Integration

### Task 2: Replace checkDiminishingReturns with BudgetTracker

**Files:**
- Modify: `internal/agent/loopstate.go`

**Code:**

Replace the `checkDiminishingReturns` method and related fields with a `BudgetTracker`:

```go
type loopState struct {
	maxTurns                  int
	turnCount                 int
	repeatedToolRounds        int
	lastToolSignature         string
	streamErr                 bool
	promptTooLongAttempts     int
	maxTokensRecoveryAttempts int
	continuationCount         int
	lastDeltaTokens           int
	lastGlobalOutputTokens    int
	lastContinueReason        ContinueReason
	nudgeEmitted              bool
	maxOutputTokens           int
	withheldErrors            *withheldErrorBuffer
	budgetTracker             *BudgetTracker
}

func newLoopState(maxTurns, turnCount, maxOutputTokens int) *loopState {
	return &loopState{
		maxTurns:        maxTurns,
		turnCount:       turnCount,
		maxOutputTokens: maxOutputTokens,
		withheldErrors:  &withheldErrorBuffer{},
		budgetTracker:   NewBudgetTracker(),
	}
}
```

Remove the old `checkDiminishingReturns` method entirely. The caller in `agent.go` will use `CheckTokenBudget` directly.

**Command:**
```bash
go test ./internal/agent/... -run TestLoopState -v
```

**Expected:** Compile passes. Existing loopState tests pass (or need update if they tested `checkDiminishingReturns`).

- [ ] **Step 1: Modify loopState to use BudgetTracker**
- [ ] **Step 2: Run tests to verify compilation**
- [ ] **Step 3: Commit**

```bash
git add internal/agent/loopstate.go
git commit -m "[STRUCTURAL] Replace checkDiminishingReturns with BudgetTracker in loopState"
```

---

## Chunk 3: Agent Loop Integration

### Task 3: Wire CheckTokenBudget into runLoop

**Files:**
- Modify: `internal/agent/agent.go:1920-1935`

Replace the existing `checkDiminishingReturns` call with `CheckTokenBudget`:

```go
		// Check token budget before executing tools
		budget := a.context.Budget()
		dec := CheckTokenBudget(ls.budgetTracker, "", budget.EffectiveWindowPtr(), totalOutputTokens)
		if dec.Action == BudgetStop {
			reason := "completion threshold"
			if dec.CompletionEvent != nil && dec.CompletionEvent.DiminishingReturns {
				reason = "diminishing returns"
			}
			a.logger.Warn("token budget stop: %s (%d%%)", reason, dec.Pct)
			a.executeTools(ctx, ch, pendingTools, streamedResults)
			a.emit(ctx, ch, TurnEvent{Type: "diminishing_returns"})
			exitReason := agentsdk.ExitDiminishingReturns
			if dec.CompletionEvent != nil && !dec.CompletionEvent.DiminishingReturns {
				exitReason = agentsdk.ExitBudgetExceeded
			}
			a.emit(ctx, ch, a.makeDoneEvent(totalInputTokens, totalOutputTokens, exitReason))
			return
		}

		// Inject budget nudge if provided and not already emitted
		if dec.NudgeMessage != "" && !ls.nudgeEmitted {
			a.conversation.AddUser(dec.NudgeMessage)
			ls.nudgeEmitted = true
		}
```

**Note:** The existing `BudgetNudge` on `ContextManager` stays but is augmented. The `CheckTokenBudget` nudge replaces the old `BudgetNudge` call at line 1962-1965. Remove or comment out:

```go
		// if nudge := a.context.BudgetNudge(a.conversation); nudge != "" && !ls.nudgeEmitted {
		// 	a.conversation.AddUser(nudge)
		// 	ls.nudgeEmitted = true
		// }
```

**Command:**
```bash
go test ./internal/agent/... -run TestRunLoop -v
```

**Expected:** Tests pass. The token budget check now uses `CheckTokenBudget`.

- [ ] **Step 1: Replace checkDiminishingReturns call with CheckTokenBudget**
- [ ] **Step 2: Remove old BudgetNudge call**
- [ ] **Step 3: Run tests to verify**
- [ ] **Step 4: Commit**

```bash
git add internal/agent/agent.go
git commit -m "[BEHAVIORAL] Integrate CheckTokenBudget into runLoop"
```

---

## Validation Commands

```bash
go test ./internal/agent/...
go test -cover ./internal/agent/...
golangci-lint run ./internal/agent/...
gofmt -l .
```

---

## PR Description

**Title:** `[BEHAVIORAL] Token Budget Tracker with diminishing returns detection`

**Body:**
- `BudgetTracker` tracks continuation count, last delta tokens, last global turn tokens
- `CheckTokenBudget` evaluates continue/stop based on:
  - Completion threshold: stop at 90% budget usage
  - Diminishing returns: 4+ consecutive turns with <500 output tokens delta
- `TokenBudgetDecision` with nudge message at 70-95% usage
- `CompletionEvent` with duration, pct, tokens, diminishing returns flag
- Replaces existing `checkDiminishingReturns` in `loopstate.go`
- Augments existing `BudgetNudge` on `ContextManager`
- Ports ccgo's `query/token_budget.go` to rubichan

**Commit prefix:** `[BEHAVIORAL]`
