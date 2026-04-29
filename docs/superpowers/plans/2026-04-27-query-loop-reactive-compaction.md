# Query Loop: Reactive Compaction on Context Overflow

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When the provider returns a `prompt_too_long` / `context_length_exceeded` error, run compaction mid-loop and retry the same turn instead of terminating.

**Architecture:** Add a `reactiveCompactThenRetry` helper that runs compaction, decrements the turn counter, and continues the loop. Integrate into `runLoop`'s error handling path using the error classifier from the previous plan.

**Tech Stack:** Go, existing `ContextManager.Compact()`, `errorclass` package

**Depends on:** `2026-04-27-query-loop-error-classifier.md`

---

## File Structure

| File | Responsibility |
|---|---|
| Create: `internal/agent/reactive_compact.go` | `reactiveCompactThenRetry()` helper + `contextCollapseDrain()` |
| Create: `internal/agent/reactive_compact_test.go` | Tests for reactive compaction recovery |
| Modify: `internal/agent/agent.go` | Wire reactive compaction into runLoop error path |
| Modify: `pkg/agentsdk/exit_reason.go` | Add `ExitContextOverflow` reason |

---

### Task 1: Add ExitContextOverflow exit reason

**Files:**
- Modify: `pkg/agentsdk/exit_reason.go`

- [ ] **Step 1: Add the constant and string method**

Add after `ExitCompactionFailed`:

```go
ExitContextOverflow
```

Add in `String()`:

```go
case ExitContextOverflow:
    return "context_overflow"
```

- [ ] **Step 2: Run tests**

Run: `go test ./pkg/agentsdk/... -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add pkg/agentsdk/exit_reason.go
git commit -m "[STRUCTURAL] Add ExitContextOverflow exit reason"
```

---

### Task 2: Implement reactiveCompactThenRetry helper

**Files:**
- Create: `internal/agent/reactive_compact.go`
- Create: `internal/agent/reactive_compact_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/agent/reactive_compact_test.go
package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/julianshen/rubichan/internal/agent/errorclass"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReactiveCompact_ReducesMessages(t *testing.T) {
	cm := NewContextManager(1000, 100)
	conv := NewConversation()
	for i := 0; i < 20; i++ {
		conv.AddUserMessage([]byte(fmt.Sprintf("message %d with enough content to have tokens", i)))
		conv.AddAssistantMessage([]provider.ContentBlock{{Type: "text", Text: "response"}})
	}
	initialLen := conv.Len()
	result := reactiveCompact(context.Background(), cm, conv)
	assert.True(t, result.compacted, "should have compacted")
	assert.Less(t, conv.Len(), initialLen, "messages should be reduced")
}

func TestContextCollapseDrain(t *testing.T) {
	msgs := make([]int, 20)
	drained := contextCollapseDrain(msgs, 5)
	assert.Less(t, len(drained), len(msgs))
	assert.GreaterOrEqual(t, len(drained), 10) // keeps minPairs*2
}

func TestClassifyAndRecover_PromptTooLong(t *testing.T) {
	err := errors.New("prompt is too long: 300000 tokens")
	class := errorclass.Classify(err)
	assert.Equal(t, errorclass.ClassPromptTooLong, class)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/... -run TestReactiveCompact -v`
Expected: FAIL — `reactiveCompact` undefined

- [ ] **Step 3: Write implementation**

```go
// internal/agent/reactive_compact.go
package agent

import (
	"context"
)

type reactiveResult struct {
	compacted bool
}

func reactiveCompact(ctx context.Context, cm *ContextManager, conv *Conversation) reactiveResult {
	if err := cm.Compact(ctx, conv); err != nil {
		return reactiveResult{}
	}
	if conv.Len() == 0 {
		return reactiveResult{}
	}
	return reactiveResult{compacted: true}
}

func contextCollapseDrain[T any](messages []T, minPairsToKeep int) []T {
	if len(messages) <= minPairsToKeep*2 {
		return messages
	}
	pairsToRemove := (len(messages) - minPairsToKeep*2) / 2
	if pairsToRemove <= 0 {
		return messages
	}
	cutoff := pairsToRemove * 2
	if cutoff >= len(messages) {
		return messages
	}
	return messages[cutoff:]
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/agent/... -run TestReactiveCompact -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/reactive_compact.go internal/agent/reactive_compact_test.go
git commit -m "[STRUCTURAL] Add reactive compaction helper for context overflow recovery"
```

---

### Task 3: Wire reactive compaction into runLoop

**Files:**
- Modify: `internal/agent/agent.go` — in the provider error path after `TurnRetry`

- [ ] **Step 1: Write the failing test**

```go
func TestRunLoop_PromptTooLong_RecoversWithCompaction(t *testing.T) {
	callCount := 0
	prov := &providerFuncMock{fn: func(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
		callCount++
		if callCount == 1 {
			return nil, fmt.Errorf("prompt is too long: 300000 tokens")
		}
		ch := make(chan provider.StreamEvent, 2)
		ch <- provider.StreamEvent{Type: "text_delta", Text: "recovered"}
		ch <- provider.StreamEvent{Type: "done", InputTokens: 1, OutputTokens: 1}
		close(ch)
		return ch, nil
	}}
	agent := newTestAgentWithProvider(prov)
	ch := agent.Turn(context.Background(), "hello")
	var exitReason agentsdk.TurnExitReason
	for evt := range ch {
		if evt.Type == "done" {
			exitReason = evt.ExitReason
		}
	}
	assert.Equal(t, agentsdk.ExitCompleted, exitReason)
	assert.Equal(t, 2, callCount, "should retry after reactive compaction")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/... -run TestRunLoop_PromptTooLong_Recovers -v`
Expected: FAIL — currently exits with `ExitProviderError` on first call

- [ ] **Step 3: Modify runLoop error path**

In `agent.go`, after the `TurnRetry` error handling (around line 1408), replace the simple return with:

```go
if err != nil {
    class := errorclass.Classify(err)
    a.logger.Warn("provider error classified as %s: %v", class, err)

    if class == errorclass.ClassPromptTooLong {
        result := reactiveCompact(ctx, a.context, a.conversation)
        if result.compacted {
            turnCount--
            continue
        }
        drained := contextCollapseDrain(a.conversation.Messages(), 5)
        if len(drained) < a.conversation.Len() {
            a.conversation.ReplaceMessages(drained)
            turnCount--
            continue
        }
        a.emit(ctx, ch, TurnEvent{Type: "error", Error: err})
        a.emit(ctx, ch, a.makeDoneEvent(totalInputTokens, totalOutputTokens, agentsdk.ExitContextOverflow))
        return
    }

    a.emit(ctx, ch, TurnEvent{Type: "error", Error: err})
    a.emit(ctx, ch, a.makeDoneEvent(totalInputTokens, totalOutputTokens, agentsdk.ExitProviderError))
    return
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/agent/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/agent.go
git commit -m "[BEHAVIORAL] Recover from prompt_too_long via reactive compaction and retry"
```
