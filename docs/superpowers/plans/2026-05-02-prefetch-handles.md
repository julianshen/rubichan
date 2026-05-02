# Prefetch Handles for Async Loading

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Load memory and skills asynchronously while the model is running, overlapping I/O with computation. Results are consumed after tool execution.

**Architecture:** Port ccgo's `query/prefetch.go:1-108`. `MemoryPrefetchHandle` and `SkillPrefetchHandle` use channel-based synchronization. The main loop starts prefetches before the LLM call and consumes results after tool execution.

**Tech Stack:** Go, existing `knowledgegraph` and `skills` packages.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/agent/prefetch.go` | `MemoryPrefetchHandle`, `SkillPrefetchHandle`, `PrefetchManager` |
| `internal/agent/prefetch_test.go` | Tests for async loading and consumption |
| `internal/agent/agent.go` | Wire prefetch into runLoop |

---

## Chunk 1: Prefetch Types

### Task 1: Define prefetch handles

**Files:**
- Create: `internal/agent/prefetch.go`

**Code:**

```go
package agent

import (
	"context"
	"sync"
	
	"github.com/julianshen/rubichan/pkg/knowledgegraph"
	"github.com/julianshen/rubichan/internal/skills"
)

// MemoryPrefetchHandle tracks an in-flight memory load.
type MemoryPrefetchHandle struct {
	Done   chan struct{}
	Result []knowledgegraph.Entity
	Err    error
}

// SkillPrefetchHandle tracks an in-flight skill load.
type SkillPrefetchHandle struct {
	Done     chan struct{}
	Fragments []skills.PromptFragment
	Err      error
}

// PrefetchManager coordinates async loading of memory and skills.
type PrefetchManager struct {
	kgSelector   *knowledgegraph.Selector
	skillRuntime *skills.Runtime
}

// NewPrefetchManager creates a manager with the given dependencies.
func NewPrefetchManager(kg *knowledgegraph.Selector, sr *skills.Runtime) *PrefetchManager {
	return &PrefetchManager{
		kgSelector:   kg,
		skillRuntime: sr,
	}
}

// StartMemoryPrefetch begins async loading of knowledge graph entities.
func (pm *PrefetchManager) StartMemoryPrefetch(ctx context.Context, query string, budget int) *MemoryPrefetchHandle {
	handle := &MemoryPrefetchHandle{Done: make(chan struct{})}
	
	go func() {
		defer close(handle.Done)
		if pm.kgSelector == nil {
			return
		}
		
		entities, err := pm.kgSelector.Select(ctx, query, budget)
		handle.Result = entities
		handle.Err = err
	}()
	
	return handle
}

// StartSkillPrefetch begins async loading of skill prompt fragments.
func (pm *PrefetchManager) StartSkillPrefetch(ctx context.Context, triggerCtx skills.TriggerContext) *SkillPrefetchHandle {
	handle := &SkillPrefetchHandle{Done: make(chan struct{})}
	
	go func() {
		defer close(handle.Done)
		if pm.skillRuntime == nil {
			return
		}
		
		fragments, err := pm.skillRuntime.EvaluateAndActivateAsync(ctx, triggerCtx)
		handle.Fragments = fragments
		handle.Err = err
	}()
	
	return handle
}

// ConsumeMemory waits for and returns the prefetch result.
func (h *MemoryPrefetchHandle) Consume(ctx context.Context) ([]knowledgegraph.Entity, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-h.Done:
		return h.Result, h.Err
	}
}

// Consume waits for and returns the prefetch result.
func (h *SkillPrefetchHandle) Consume(ctx context.Context) ([]skills.PromptFragment, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-h.Done:
		return h.Fragments, h.Err
	}
}
```

**Test:**

```go
func TestPrefetchManager(t *testing.T) {
	pm := NewPrefetchManager(nil, nil)
	
	ctx := context.Background()
	memHandle := pm.StartMemoryPrefetch(ctx, "test", 1000)
	skillHandle := pm.StartSkillPrefetch(ctx, skills.TriggerContext{})
	
	// Should complete immediately with nil deps
	entities, err := memHandle.Consume(ctx)
	require.NoError(t, err)
	require.Nil(t, entities)
	
	fragments, err := skillHandle.Consume(ctx)
	require.NoError(t, err)
	require.Nil(t, fragments)
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestPrefetchManager -v
```

**Expected:** PASS.

---

## Chunk 2: Integration

### Task 2: Wire prefetch into runLoop

**Files:**
- Modify: `internal/agent/agent.go`

**Code:**

```go
func (a *Agent) runLoop(ctx context.Context, ch chan<- TurnEvent, turnCount int, lastUserMessage string) {
	// ... existing setup ...
	
	for ; ls.hasMoreTurns(); ls.turnCount++ {
		// Start async prefetches before LLM call
		var memHandle *MemoryPrefetchHandle
		var skillHandle *SkillPrefetchHandle
		
		if a.prefetchMgr != nil {
			memHandle = a.prefetchMgr.StartMemoryPrefetch(ctx, lastUserMessage, budget.SkillPrompts)
			skillHandle = a.prefetchMgr.StartSkillPrefetch(ctx, a.buildSkillTriggerContext(lastUserMessage))
		}
		
		// ... existing LLM call and stream processing ...
		
		// After tool execution, consume prefetch results
		if memHandle != nil {
			entities, err := memHandle.Consume(ctx)
			if err == nil && len(entities) > 0 {
				// Inject into next turn's context
				a.lastPrefetchedEntities = entities
			}
		}
		
		if skillHandle != nil {
			fragments, err := skillHandle.Consume(ctx)
			if err == nil {
				a.lastPrefetchedFragments = fragments
			}
		}
		
		// ... rest of loop ...
	}
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestRunLoop -v
```

**Expected:** Existing tests pass.

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

**Title:** `[BEHAVIORAL] Prefetch handles for async memory/skill loading`

**Body:**
- `MemoryPrefetchHandle` and `SkillPrefetchHandle` load data async while model runs
- `PrefetchManager` coordinates parallel I/O with LLM computation
- Results consumed after tool execution, injected into next turn
- Channel-based synchronization with context cancellation support
- Ports ccgo's `query/prefetch.go:1-108` pattern to Go

**Commit prefix:** `[BEHAVIORAL]`
