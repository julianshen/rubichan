# Query Loop: Turn Generation Counter

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a generation counter to `Turn()` so stale cleanup from a cancelled turn cannot corrupt a subsequent turn's state.

**Architecture:** An atomic `generation` counter on `Agent` incremented at the start of each `Turn()` call. Deferred cleanup checks the generation before applying side effects.

**Tech Stack:** Go, `sync/atomic`

---

## File Structure

| File | Responsibility |
|---|---|
| Modify: `internal/agent/agent.go` | Add generation counter to Agent struct, increment in Turn |
| Create: `internal/agent/generation_test.go` | Test generation counter behavior |

---

### Task 1: Add generation counter

**Files:**
- Modify: `internal/agent/agent.go`
- Create: `internal/agent/generation_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/agent/generation_test.go
package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAgent_GenerationIncrementsPerTurn(t *testing.T) {
	prov := &providerFuncMock{fn: func(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
		ch := make(chan provider.StreamEvent, 2)
		ch <- provider.StreamEvent{Type: "text_delta", Text: "hi"}
		ch <- provider.StreamEvent{Type: "done", InputTokens: 1, OutputTokens: 1}
		close(ch)
		return ch, nil
	}}
	agent := newTestAgentWithProvider(prov)
	genBefore := agent.Generation()
	ch := agent.Turn(context.Background(), "hello")
	for range ch {
	}
	genAfter := agent.Generation()
	assert.Equal(t, genBefore+1, genAfter, "generation should increment after Turn")
}

func TestAgent_GenerationDifferentAcrossTurns(t *testing.T) {
	prov := &providerFuncMock{fn: func(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
		ch := make(chan provider.StreamEvent, 2)
		ch <- provider.StreamEvent{Type: "text_delta", Text: "hi"}
		ch <- provider.StreamEvent{Type: "done", InputTokens: 1, OutputTokens: 1}
		close(ch)
		return ch, nil
	}}
	agent := newTestAgentWithProvider(prov)
	ch := agent.Turn(context.Background(), "first")
	for range ch {
	}
	gen1 := agent.Generation()
	ch = agent.Turn(context.Background(), "second")
	for range ch {
	}
	gen2 := agent.Generation()
	assert.Equal(t, gen1+1, gen2, "generation should increment across turns")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/... -run TestAgent_Generation -v`
Expected: FAIL — `agent.Generation` undefined

- [ ] **Step 3: Add generation field and methods**

Add to the `Agent` struct in `agent.go`:

```go
generation atomic.Int64
```

Add methods:

```go
func (a *Agent) Generation() int64 {
	return a.generation.Load()
}
```

In `Turn()`, before launching the goroutine, add:

```go
gen := a.generation.Add(1)
```

Pass `gen` to the goroutine and store it for future use in stale-check logic. For now, just incrementing and exposing is sufficient.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/agent/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/agent.go internal/agent/generation_test.go
git commit -m "[STRUCTURAL] Add generation counter to Agent for stale-turn detection"
```
