# Query Source-Aware Retry Classification

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent background tasks (summaries, classifiers, compaction) from retrying on 529 (server overloaded), which amplifies capacity cascades. Foreground tasks (user queries) continue retrying.

**Architecture:** Port Claude Code's `FOREGROUND_529_RETRY_SOURCES` pattern from `withRetry.ts:57-89`. A `QuerySource` enum classifies requests. The retry layer checks the source before retrying on 529.

**Tech Stack:** Go, existing `DoWithRetryConfig` and `TurnRetryConfig`.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `pkg/agentsdk/query_source.go` | `QuerySource` enum and classification |
| `internal/provider/retry.go` | Wire source check into `DoWithRetry` |
| `internal/agent/turnretry.go` | Wire source check into `TurnRetry` |

---

## Chunk 1: Query Source Classification

### Task 1: Define QuerySource enum

**Files:**
- Create: `pkg/agentsdk/query_source.go`

**Code:**

```go
package agentsdk

// QuerySource classifies the origin of a request for retry behavior.
// Background tasks should not retry on 529 to avoid amplifying overload.
type QuerySource int

const (
	// QuerySourceForeground is a user-facing query. Retries on 529
	// are appropriate because the user is waiting.
	QuerySourceForeground QuerySource = iota
	// QuerySourceBackground is a background task (summary, classifier,
	// compaction). Fails fast on 529 to avoid amplifying capacity cascades.
	QuerySourceBackground
	// QuerySourceHook is a hook-initiated request. Treated as background
	// unless explicitly marked foreground.
	QuerySourceHook
)

func (s QuerySource) String() string {
	switch s {
	case QuerySourceForeground:
		return "foreground"
	case QuerySourceBackground:
		return "background"
	case QuerySourceHook:
		return "hook"
	default:
		return "unknown"
	}
}

// ShouldRetryOn529 returns whether this source should retry when the
// server returns 529 (overloaded). Only foreground queries retry.
func (s QuerySource) ShouldRetryOn529() bool {
	return s == QuerySourceForeground
}
```

**Test:**

```go
func TestQuerySourceShouldRetryOn529(t *testing.T) {
	require.True(t, QuerySourceForeground.ShouldRetryOn529())
	require.False(t, QuerySourceBackground.ShouldRetryOn529())
	require.False(t, QuerySourceHook.ShouldRetryOn529())
}
```

**Command:**
```bash
go test ./pkg/agentsdk/... -run TestQuerySourceShouldRetryOn529 -v
```

**Expected:** PASS.

---

## Chunk 2: Retry Layer Integration

### Task 2: Add QuerySource to DoWithRetryConfig

**Files:**
- Modify: `internal/provider/retry.go`

**Code:**

```go
type DoWithRetryConfig struct {
	// ... existing fields ...
	
	// Source classifies the request for retry behavior. Background
	// tasks fail fast on 529 to avoid amplifying server overload.
	Source agentsdk.QuerySource
}

// isRetryableError now checks the source for 529 errors.
func isRetryableError(err error, source agentsdk.QuerySource) bool {
	if err == nil {
		return false
	}
	
	// Check for 529 overloaded
	if isOverloadedError(err) {
		return source.ShouldRetryOn529()
	}
	
	// Other errors use existing classification
	return isTransientError(err)
}
```

**Command:**
```bash
go test ./internal/provider/... -run TestDoWithRetry -v
```

**Expected:** Existing tests pass.

---

### Task 3: Add QuerySource to TurnRetryConfig

**Files:**
- Modify: `internal/agent/turnretry.go`

**Code:**

```go
type TurnRetryConfig struct {
	// ... existing fields ...
	
	// Source classifies the turn for retry behavior.
	Source agentsdk.QuerySource
}

func TurnRetry(ctx context.Context, cfg TurnRetryConfig, op StreamingOp, onRetry OnRetry) (<-chan provider.StreamEvent, error) {
	// Pass source to DoWithRetry
	doCfg := DoWithRetryConfig{
		// ... existing fields ...
		Source: cfg.Source,
	}
	// ... rest of implementation ...
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestTurnRetry -v
```

**Expected:** Existing tests pass.

---

## Chunk 3: Agent Integration

### Task 4: Classify requests in agent.go

**Files:**
- Modify: `internal/agent/agent.go`

**Code:**

```go
func (a *Agent) Turn(ctx context.Context, userMessage string) (<-chan TurnEvent, error) {
	// Foreground query
	return a.runLoop(ctx, userMessage, agentsdk.QuerySourceForeground)
}

func (a *Agent) runLoop(ctx context.Context, userMessage string, source agentsdk.QuerySource) (<-chan TurnEvent, error) {
	// ... existing setup ...
	
	retryCfg := TurnRetryConfig{
		// ... existing fields ...
		Source: source,
	}
	
	stream, err := TurnRetry(ctx, retryCfg, /* ... */)
	// ... rest of implementation ...
}
```

For background tasks (compaction, summaries):
```go
func (a *Agent) compactConversation(ctx context.Context) error {
	// Background task — fail fast on 529
	retryCfg := TurnRetryConfig{
		Source: agentsdk.QuerySourceBackground,
	}
	// ...
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestTurnRetry -v
```

**Expected:** All tests pass.

---

## Validation Commands

```bash
go test ./pkg/agentsdk/...
go test ./internal/provider/...
go test ./internal/agent/...
go test -cover ./internal/agent/...
golangci-lint run ./internal/agent/...
gofmt -l .
```

---

## PR Description

**Title:** `[BEHAVIORAL] Query source-aware retry classification`

**Body:**
- Add `QuerySource` enum: foreground, background, hook
- Only foreground queries retry on 529 (server overloaded)
- Background tasks (summaries, classifiers, compaction) fail fast to avoid amplifying capacity cascades
- Ports Claude Code's `FOREGROUND_529_RETRY_SOURCES` pattern from `withRetry.ts:57-89`

**Commit prefix:** `[BEHAVIORAL]`
