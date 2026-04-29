# Query Loop: LoopState Extraction

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract mutable loop state from `runLoop`'s inline variables into a dedicated `loopState` struct, making recovery paths independently testable.

**Architecture:** A `loopState` struct holds turn counter, recovery attempt counters, stop reason, stream error flag, and pending tool tracking. Created once at the top of `runLoop`, passed through helper functions.

**Tech Stack:** Go

---

## File Structure

| File | Responsibility |
|---|---|
| Create: `internal/agent/loopstate.go` | `loopState` struct definition |
| Create: `internal/agent/loopstate_test.go` | Tests for loopState methods |
| Modify: `internal/agent/agent.go` | Replace inline vars with `loopState` struct |

---

### Task 1: Define loopState struct

**Files:**
- Create: `internal/agent/loopstate.go`
- Create: `internal/agent/loopstate_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/agent/loopstate_test.go
package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoopState_CanRecoverMaxTokens(t *testing.T) {
	ls := newLoopState(10)
	assert.True(t, ls.canRecoverMaxTokens())
	ls.maxTokensRecoveryAttempts = 3
	assert.False(t, ls.canRecoverMaxTokens())
}

func TestLoopState_IncrementRecovery(t *testing.T) {
	ls := newLoopState(10)
	ls.incrementMaxTokensRecovery()
	assert.Equal(t, 1, ls.maxTokensRecoveryAttempts)
	ls.incrementMaxTokensRecovery()
	assert.Equal(t, 2, ls.maxTokensRecoveryAttempts)
}

func TestLoopState_ShouldExit(t *testing.T) {
	ls := newLoopState(2)
	assert.False(t, ls.shouldExit())
	ls.turnCount = 2
	assert.True(t, ls.shouldExit())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/... -run TestLoopState -v`
Expected: FAIL — `newLoopState` undefined

- [ ] **Step 3: Write implementation**

```go
// internal/agent/loopstate.go
package agent

const maxOutputTokensRecoveryLimit = 3

type loopState struct {
	maxTurns                  int
	turnCount                 int
	maxTokensRecoveryAttempts int
	hasEscalatedMaxTokens     bool
	repeatedToolRounds        int
	lastToolSignature         string
	streamErr                 bool
}

func newLoopState(maxTurns int) *loopState {
	return &loopState{maxTurns: maxTurns}
}

func (s *loopState) canRecoverMaxTokens() bool {
	return s.maxTokensRecoveryAttempts < maxOutputTokensRecoveryLimit
}

func (s *loopState) incrementMaxTokensRecovery() {
	s.maxTokensRecoveryAttempts++
}

func (s *loopState) shouldExit() bool {
	return s.turnCount >= s.maxTurns
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/... -run TestLoopState -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/loopstate.go internal/agent/loopstate_test.go
git commit -m "[STRUCTURAL] Add loopState struct for runLoop mutable state"
```

---

### Task 2: Migrate runLoop inline vars to loopState

**Files:**
- Modify: `internal/agent/agent.go`

- [ ] **Step 1: Write characterization test**

```go
func TestRunLoop_LoopStateIntegration(t *testing.T) {
	prov := &providerFuncMock{fn: func(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
		ch := make(chan provider.StreamEvent, 2)
		ch <- provider.StreamEvent{Type: "text_delta", Text: "hello"}
		ch <- provider.StreamEvent{Type: "done", InputTokens: 1, OutputTokens: 1}
		close(ch)
		return ch, nil
	}}
	agent := newTestAgentWithProvider(prov)
	ch := agent.Turn(context.Background(), "test")
	var gotDone bool
	for evt := range ch {
		if evt.Type == "done" {
			gotDone = true
		}
	}
	assert.True(t, gotDone)
}
```

- [ ] **Step 2: Run test**

Run: `go test ./internal/agent/... -run TestRunLoop_LoopStateIntegration -v`
Expected: PASS (characterizes current behavior)

- [ ] **Step 3: Replace inline vars with loopState**

In `agent.go` `runLoop`, replace:

```go
var lastPendingToolSignature string
repeatedPendingToolRounds := 0
```

with:

```go
ls := newLoopState(a.maxTurns)
ls.turnCount = turnCount
```

And update all references throughout runLoop:
- `lastPendingToolSignature` → `ls.lastToolSignature`
- `repeatedPendingToolRounds` → `ls.repeatedToolRounds`
- `streamErr` → `ls.streamErr`
- `turnCount < a.maxTurns` → `!ls.shouldExit()` in the for condition

- [ ] **Step 4: Run all agent tests**

Run: `go test ./internal/agent/... -v -count=1`
Expected: PASS — no behavioral change

- [ ] **Step 5: Commit**

```bash
git add internal/agent/agent.go
git commit -m "[STRUCTURAL] Migrate runLoop inline vars to loopState struct"
```
