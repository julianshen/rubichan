# Sibling Abort on Bash Errors

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When a shell/bash tool fails in a concurrent batch, abort all sibling tools to prevent wasted work. Other tool failures (read_file, grep, etc.) are independent and do not cascade.

**Architecture:** Port Claude Code's sibling abort pattern from `StreamingToolExecutor.ts:356-363`. Each concurrent batch gets a child `AbortController`. When a shell tool errors, the child controller fires, cancelling all siblings. Non-shell errors only cancel the failing tool.

**Tech Stack:** Go, existing `streamingToolExecutor`, `context.Context` cancellation.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/agent/stream_tool_exec.go` | Add `siblingAbortController` to `streamingToolExecutor` |
| `internal/agent/stream_tool_exec_test.go` | Tests for sibling abort behavior |
| `internal/toolexec/batch.go` | Wire sibling abort into `BatchExecutor` |

---

## Chunk 1: Sibling Abort in Streaming Executor

### Task 1: Add sibling abort controller to streamingToolExecutor

**Files:**
- Modify: `internal/agent/stream_tool_exec.go`

**Code:**

```go
// streamingToolExecutor executes concurrency-safe tools in parallel during
// the model stream. It now supports sibling abort: when a shell tool errors,
// all other in-flight tools in the same batch are cancelled.
type streamingToolExecutor struct {
	// ... existing fields ...
	
	// siblingAbort is triggered when any shell tool errors. All non-shell
	// siblings receive this cancellation. Non-shell errors do not trigger
	// sibling abort — only the failing tool is cancelled.
	siblingAbort context.CancelFunc
}
```

**Test:**

```go
func TestStreamingExecutor_SiblingAbortOnShellError(t *testing.T) {
	t.Parallel()
	// Two concurrent tools: shell (will error) and read_file (should be aborted)
	shell := &fakeConcurrencySafeTool{name: "shell", execDelay: 50 * time.Millisecond, returnText: "error"}
	read := &fakeConcurrencySafeTool{name: "read_file", execDelay: 100 * time.Millisecond, returnText: "ok"}
	
	ex := newExecutorWithTools(2, map[string]agentsdk.Tool{"shell": shell, "read_file": read})
	ctx := context.Background()
	
	ex.Dispatch(ctx, provider.ToolUseBlock{ID: "s1", Name: "shell"})
	ex.Dispatch(ctx, provider.ToolUseBlock{ID: "r1", Name: "read_file"})
	results := ex.Drain()
	
	require.Len(t, results, 2)
	// Shell result is an error
	shellResult := results[0] // or find by ID
	require.True(t, shellResult.isError)
	// Read was aborted by sibling error
	readResult := results[1]
	require.True(t, readResult.isError)
	require.Contains(t, readResult.content, "aborted")
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestStreamingExecutor_SiblingAbortOnShellError -v
```

**Expected:** Test fails — sibling abort not yet implemented.

---

### Task 2: Implement sibling abort logic in Dispatch

**Files:**
- Modify: `internal/agent/stream_tool_exec.go`

**Code:**

In `newStreamingToolExecutor`, initialize sibling abort:
```go
func newStreamingToolExecutor(maxParallel int, run toolExecFn) *streamingToolExecutor {
	// ... existing init ...
	_, siblingCancel := context.WithCancel(context.Background())
	return &streamingToolExecutor{
		// ... existing fields ...
		siblingAbort: siblingCancel,
	}
}
```

In the goroutine spawned by `Dispatch`, check for shell errors:
```go
// After run() returns:
if res.isError && isShellTool(tc.Name) {
	e.siblingAbort()
}
```

Add `isShellTool` helper:
```go
func isShellTool(name string) bool {
	return name == "shell" || name == "bash" || name == "cmd"
}
```

Pass sibling context to tool execution:
```go
// In Dispatch goroutine:
toolCtx, toolCancel := context.WithCancel(ctx)
select {
case <-e.siblingAbortCtx.Done():
	// Sibling error occurred, abort this tool
	res = toolErrorResult(tc, "aborted: sibling shell tool failed")
case res = <-runAsync(toolCtx, tc):
	// Normal completion
}
```

**Test:** Re-run Task 1 test — should pass.

**Command:**
```bash
go test ./internal/agent/... -run TestStreamingExecutor_SiblingAbortOnShellError -v
```

**Expected:** PASS.

---

### Task 3: Non-shell errors do not trigger sibling abort

**Test:**

```go
func TestStreamingExecutor_NonShellErrorNoSiblingAbort(t *testing.T) {
	t.Parallel()
	// read_file errors, but grep should complete normally
	read := &fakeConcurrencySafeTool{name: "read_file", execDelay: 50 * time.Millisecond, returnText: "error"}
	grep := &fakeConcurrencySafeTool{name: "grep", execDelay: 100 * time.Millisecond, returnText: "ok"}
	
	ex := newExecutorWithTools(2, map[string]agentsdk.Tool{"read_file": read, "grep": grep})
	ctx := context.Background()
	
	ex.Dispatch(ctx, provider.ToolUseBlock{ID: "r1", Name: "read_file"})
	ex.Dispatch(ctx, provider.ToolUseBlock{ID: "g1", Name: "grep"})
	results := ex.Drain()
	
	require.Len(t, results, 2)
	readResult := findResult(results, "r1")
	grepResult := findResult(results, "g1")
	require.True(t, readResult.isError)
	require.False(t, grepResult.isError, "non-shell error should not abort siblings")
	require.Equal(t, "ok", grepResult.content)
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestStreamingExecutor_NonShellErrorNoSiblingAbort -v
```

**Expected:** PASS.

---

## Chunk 2: Wire into Batch Executor

### Task 4: Propagate sibling abort through BatchExecutor

**Files:**
- Modify: `internal/toolexec/batch.go`

**Code:**

```go
// BatchExecutor now accepts a sibling abort channel. When a shell tool
// errors in a concurrent batch, the entire batch is cancelled.
type BatchExecutor struct {
	// ... existing fields ...
	siblingAbort context.CancelFunc
}

func (be *BatchExecutor) ExecuteBatch(ctx context.Context, batch ToolBatch) []toolExecResult {
	if !batch.ConcurrentSafe {
		// Sequential batch: no sibling abort needed
		return be.executeSequential(ctx, batch)
	}
	
	// Concurrent batch: create sibling abort context
	siblingCtx, siblingCancel := context.WithCancel(ctx)
	defer siblingCancel()
	
	var wg sync.WaitGroup
	results := make([]toolExecResult, len(batch.Tools))
	for i, tc := range batch.Tools {
		wg.Add(1)
		go func(idx int, toolCall provider.ToolUseBlock) {
			defer wg.Done()
			res := be.run(siblingCtx, toolCall)
			if res.isError && isShellTool(toolCall.Name) {
				siblingCancel() // Abort siblings
			}
			results[idx] = res
		}(i, tc)
	}
	wg.Wait()
	return results
}
```

**Test:**

```go
func TestBatchExecutor_SiblingAbort(t *testing.T) {
	// Shell error in concurrent batch aborts siblings
	// ... test code ...
}
```

**Command:**
```bash
go test ./internal/toolexec/... -run TestBatchExecutor_SiblingAbort -v
```

**Expected:** PASS.

---

## Chunk 3: Integration

### Task 5: Wire sibling abort into agent.go executeTools

**Files:**
- Modify: `internal/agent/agent.go`

**Code:**

In `executeTools`, pass sibling abort to batch executor:
```go
func (a *Agent) executeTools(ctx context.Context, ch chan<- TurnEvent, pending []provider.ToolUseBlock, streamed map[string]toolExecResult) bool {
	// ... existing streamed merge logic ...
	
	batches := toolexec.PartitionToolCalls(planned)
	be := toolexec.NewBatchExecutor(a.tools, maxParallelTools)
	
	for _, batch := range batches {
		if ctx.Err() != nil {
			return true
		}
		results := be.ExecuteBatch(ctx, batch)
		// ... emit results ...
	}
	return false
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestExecuteTools -v
```

**Expected:** All existing tests pass + new sibling abort tests pass.

---

## Validation Commands

```bash
go test ./internal/agent/...
go test ./internal/toolexec/...
go test -cover ./internal/agent/...
golangci-lint run ./internal/agent/...
gofmt -l .
```

---

## PR Description

**Title:** `[BEHAVIORAL] Sibling abort on shell errors`

**Body:**
- When a shell/bash tool errors in a concurrent batch, all sibling tools are cancelled to prevent wasted work
- Non-shell errors (read_file, grep, etc.) do not cascade — only the failing tool is cancelled
- Ports Claude Code's `StreamingToolExecutor.ts:356-363` pattern to Go
- Adds `siblingAbort` context to `streamingToolExecutor` and `BatchExecutor`

**Commit prefix:** `[BEHAVIORAL]`
