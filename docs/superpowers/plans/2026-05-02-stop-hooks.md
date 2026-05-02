# Stop Hooks with Continuation Control

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable hooks to block loop continuation, inject messages, or yield progress. Three-phase execution: stop hooks → task completed → teammate idle.

**Architecture:** Port Claude Code's `query/stopHooks.ts:65-473`. A `StopHookRegistry` manages hooks. After each turn, run stop hooks. If any hook returns `preventContinuation`, the loop exits. If `blockingErrors`, yield as meta messages and continue.

**Tech Stack:** Go, existing hook system in `internal/hooks/`.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/hooks/stop.go` | `StopHookRegistry`, `StopHookResult`, `RunStopHooks` |
| `internal/hooks/stop_test.go` | Tests for blocking, prevention, and idle hooks |
| `internal/agent/agent.go` | Wire stop hooks into runLoop |

---

## Chunk 1: Stop Hook Types

### Task 1: Define StopHookResult and registry

**Files:**
- Create: `internal/hooks/stop.go`

**Code:**

```go
package hooks

import (
	"context"
	"sync"
	"time"
)

// StopHookResult is the aggregated outcome of running stop hooks.
type StopHookResult struct {
	// PreventContinuation stops the loop entirely.
	PreventContinuation bool
	// BlockingErrors are yielded as meta messages but don't stop.
	BlockingErrors []error
	// Messages to inject into the conversation.
	Messages []string
	// Attachments to yield to the UI.
	Attachments []Attachment
	// Duration tracks total hook execution time.
	Duration time.Duration
}

// Attachment is a file or artifact produced by a hook.
type Attachment struct {
	Name     string
	Content  string
	MimeType string
}

// StopHook is a function that runs after each turn.
type StopHook func(ctx context.Context, state HookState) (*StopHookResult, error)

// StopHookRegistry manages stop hooks.
type StopHookRegistry struct {
	mu    sync.RWMutex
	hooks []StopHook
}

// NewStopHookRegistry creates an empty registry.
func NewStopHookRegistry() *StopHookRegistry {
	return &StopHookRegistry{hooks: make([]StopHook, 0)}
}

// Register adds a stop hook.
func (r *StopHookRegistry) Register(hook StopHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks = append(r.hooks, hook)
}

// RunStopHooks executes all registered stop hooks and aggregates results.
// Hooks run sequentially; if any hook returns preventContinuation,
// subsequent hooks are skipped.
func (r *StopHookRegistry) RunStopHooks(ctx context.Context, state HookState) *StopHookResult {
	r.mu.RLock()
	hooks := make([]StopHook, len(r.hooks))
	copy(hooks, r.hooks)
	r.mu.RUnlock()
	
	result := &StopHookResult{}
	start := time.Now()
	
	for _, hook := range hooks {
		if ctx.Err() != nil {
			break
		}
		
		hookResult, err := hook(ctx, state)
		if err != nil {
			result.BlockingErrors = append(result.BlockingErrors, err)
			continue
		}
		
		if hookResult == nil {
			continue
		}
		
		if hookResult.PreventContinuation {
			result.PreventContinuation = true
			break
		}
		
		result.BlockingErrors = append(result.BlockingErrors, hookResult.BlockingErrors...)
		result.Messages = append(result.Messages, hookResult.Messages...)
		result.Attachments = append(result.Attachments, hookResult.Attachments...)
	}
	
	result.Duration = time.Since(start)
	return result
}

// HookState provides context to stop hooks.
type HookState struct {
	TurnCount    int
	ToolCalls    []string
	ResponseText string
	ExitReason   string
}
```

**Test:**

```go
func TestStopHookRegistry(t *testing.T) {
	r := NewStopHookRegistry()
	
	// Hook that prevents continuation
	r.Register(func(ctx context.Context, state HookState) (*StopHookResult, error) {
		return &StopHookResult{PreventContinuation: true}, nil
	})
	
	// This hook should not run
	r.Register(func(ctx context.Context, state HookState) (*StopHookResult, error) {
		t.Fatal("should not run after preventContinuation")
		return nil, nil
	})
	
	result := r.RunStopHooks(context.Background(), HookState{})
	require.True(t, result.PreventContinuation)
}
```

**Command:**
```bash
go test ./internal/hooks/... -run TestStopHookRegistry -v
```

**Expected:** PASS.

---

## Chunk 2: Integration

### Task 2: Wire stop hooks into runLoop

**Files:**
- Modify: `internal/agent/agent.go`

**Code:**

```go
func (a *Agent) runLoop(ctx context.Context, ch chan<- TurnEvent, turnCount int, lastUserMessage string) {
	// ... existing setup ...
	
	for ; ls.hasMoreTurns(); ls.turnCount++ {
		// ... existing turn logic ...
		
		// After turn completes, run stop hooks
		if a.stopHookRegistry != nil {
			hookState := hooks.HookState{
				TurnCount:    ls.turnCount,
				ToolCalls:    extractToolNames(pendingTools),
				ResponseText: assistantText(blocks),
				ExitReason:   exitReason.String(),
			}
			
			hookResult := a.stopHookRegistry.RunStopHooks(ctx, hookState)
			
			// Yield blocking errors as meta messages
			for _, err := range hookResult.BlockingErrors {
				a.emit(ctx, ch, TurnEvent{
					Type:  "error",
					Error: fmt.Errorf("stop hook: %w", err),
				})
			}
			
			// Inject hook messages into conversation
			for _, msg := range hookResult.Messages {
				a.conversation.AddSystem(msg)
			}
			
			// Check if hooks blocked continuation
			if hookResult.PreventContinuation {
				a.emit(ctx, ch, TurnEvent{Type: "stop_hook_prevented"})
				a.emit(ctx, ch, a.makeDoneEvent(totalInputTokens, totalOutputTokens, agentsdk.ExitStopHookPrevented))
				return
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

### Task 3: Add ExitStopHookPrevented reason

**Files:**
- Modify: `pkg/agentsdk/exit_reason.go`

**Code:**

```go
const (
	// ... existing reasons ...
	
	// ExitStopHookPrevented means a stop hook blocked continuation.
	ExitStopHookPrevented TurnExitReason = "stop_hook_prevented"
)
```

**Command:**
```bash
go test ./pkg/agentsdk/... -run TestExitReason -v
```

**Expected:** PASS.

---

## Validation Commands

```bash
go test ./internal/hooks/...
go test ./internal/agent/...
go test ./pkg/agentsdk/...
go test -cover ./internal/agent/...
golangci-lint run ./internal/agent/...
gofmt -l .
```

---

## PR Description

**Title:** `[BEHAVIORAL] Stop hooks with continuation control`

**Body:**
- Three-phase stop hook execution after each turn
- Hooks can: block continuation, inject messages, yield attachments
- `PreventContinuation` exits the loop with `ExitStopHookPrevented`
- `BlockingErrors` are yielded as meta messages but don't stop
- Ports Claude Code's `query/stopHooks.ts:65-473` pattern to Go

**Commit prefix:** `[BEHAVIORAL]`
