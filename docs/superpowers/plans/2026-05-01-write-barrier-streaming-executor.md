# Query Loop Improvements: Batch 2.8 — Write-Barrier in Streaming Executor

> **Status:** Planned, not started.  
> **Depends on:** 2.7 (InputConcurrencySafeTool) — done.  
> **Goal:** Ensure ordering correctness when the model emits mixed read/write tool sequences during streaming.

## Background

The streaming tool executor (`internal/agent/stream_tool_exec.go`) dispatches concurrency-safe tools as soon as their `tool_use` block finalizes during the model's streaming response. This overlaps slow I/O with the remaining stream, saving 1-3s per turn.

The current design assumes all concurrency-safe tools are **commutative** — their execution order doesn't matter. This is true for pure reads (read_file, grep, ls) but breaks when a write operation is followed by a read that depends on it.

### The Problem

Consider this tool sequence from the model:

```
1. write_file(path="/tmp/config.json", content="...")
2. read_file(path="/tmp/config.json")
3. grep(pattern="foo", path="/tmp")
```

With the current executor:
- `write_file` is NOT concurrency-safe → queued for post-stream execution
- `read_file` IS concurrency-safe → dispatched immediately during stream
- `grep` IS concurrency-safe → dispatched immediately during stream

**Race condition:** `read_file` and `grep` may execute *before* `write_file` completes, reading stale or missing data.

This is the **write-barrier** problem: a write operation acts as an ordering barrier. All subsequent tools (even concurrency-safe ones) must wait for the write to complete before executing.

## Reference: ccgo's Approach

ccgo solves this via `PartitionToolCalls` in `execution/executor.go`:

1. Each tool declares `IsConcurrencySafe(input)` — checked per invocation
2. Tools are partitioned into **batches** where each batch contains only safe tools
3. An **unsafe tool acts as a barrier** — the next batch starts after it completes
4. Safe batches run concurrently with a semaphore; unsafe batches run sequentially

```go
// ccgo execution/executor.go
func (e *ToolExecutor) PartitionToolCalls(toolUses []ToolUseBlock) []ToolBatch {
    var batches []ToolBatch
    var currentBatch ToolBatch
    for _, tu := range toolUses {
        t := FindToolByName(e.tools, tu.Name)
        isSafe := t.IsConcurrencySafe(tu.Input)
        if isSafe {
            currentBatch = append(currentBatch, tu)
        } else {
            if len(currentBatch) > 0 {
                batches = append(batches, currentBatch)
                currentBatch = nil
            }
            batches = append(batches, ToolBatch{tu}) // unsafe = singleton batch
        }
    }
    if len(currentBatch) > 0 {
        batches = append(batches, currentBatch)
    }
    return batches
}
```

## Design for rubichan

### Option A: Barrier-aware streaming executor (recommended)

Extend `streamingToolExecutor` to track pending barriers:

1. When an **unsafe** tool is finalized during streaming, mark it as a pending barrier
2. When a **safe** tool is finalized, check if any barrier is pending:
   - If no barrier: dispatch immediately (current behavior)
   - If barrier pending: queue the safe tool; dispatch after barrier completes
3. Post-stream `executeTools` processes barriers in order, then drains queued safe tools

```go
type streamingToolExecutor struct {
    sem       chan struct{}
    run       toolExecFn
    mu        sync.Mutex
    futures   []*toolFuture
    wg        sync.WaitGroup
    barriers  []string           // tool_use IDs of pending write barriers
    queued    []*toolFuture      // safe tools queued behind a barrier
}
```

### Option B: Two-phase dispatch (simpler, less overlap)

1. During streaming: only dispatch safe tools that appear **before** any unsafe tool
2. All tools after the first unsafe tool are queued for post-stream execution
3. Post-stream: execute barriers sequentially, then safe tools concurrently

This loses some parallelism (safe tools after a barrier wait for post-stream) but is simpler to implement correctly.

### Option C: Full batch partitioning (ccgo-style)

Replace the streaming executor with a batch-based approach:
1. Collect all tool_use blocks during streaming (don't dispatch any)
2. After stream ends, partition into batches
3. Execute safe batches concurrently, unsafe batches sequentially

This loses the streaming overlap benefit entirely — safe tools don't start until the stream ends.

## Recommendation: Option A

Option A preserves maximum parallelism while ensuring correctness. The key insight: we don't need to partition upfront — we can dispatch eagerly and retroactively queue when a barrier appears.

## Implementation Plan

### Task 1: Add `IsWriteOperation` to tool interface

Add an optional marker interface for tools that mutate state:

```go
// pkg/agentsdk/tool_caps.go
type WriteTool interface {
    IsWriteOperation() bool
}
```

Tools that implement this and return `true` act as barriers.

### Task 2: Extend streamingToolExecutor with barrier tracking

```go
// internal/agent/stream_tool_exec.go

// barrierFuture tracks a write barrier and the safe tools queued behind it.
type barrierFuture struct {
    barrierToolUseID string
    barrierDone      chan struct{}
    queued           []*toolFuture
}

func (e *streamingToolExecutor) Dispatch(ctx context.Context, tc provider.ToolUseBlock, isBarrier bool) {
    if isBarrier {
        e.dispatchBarrier(ctx, tc)
        return
    }
    e.dispatchSafe(ctx, tc)
}
```

### Task 3: Wire barrier detection into finalizeTool

In `agent.go:finalizeTool`, check if the tool is a write operation:

```go
isBarrier := false
if wt, ok := tool.(agentsdk.WriteTool); ok && wt.IsWriteOperation() {
    isBarrier = true
}

if ic, ok := tool.(agentsdk.InputConcurrencySafeTool); ok {
    safe := ic.IsConcurrencySafeForInput(currentTool.Input)
    if safe && !isBarrier {
        // dispatch immediately
        execStream.Dispatch(ctx, *currentTool, false)
    } else if safe && isBarrier {
        // queue behind barrier
        execStream.Dispatch(ctx, *currentTool, true)
    }
}
```

### Task 4: Post-stream barrier resolution

In `executeTools`, process barriers in order:
1. For each barrier, wait for it to complete
2. Then dispatch all queued safe tools behind it
3. Continue to next barrier

### Task 5: Tests

- `TestStreamingExecutor_BarrierBlocksSubsequentSafeTools`
- `TestStreamingExecutor_SafeToolsBeforeBarrierDispatchImmediately`
- `TestStreamingExecutor_MultipleBarriersInSequence`
- `TestStreamingExecutor_BarrierWithNoQueuedTools`

## Files to modify

| File | Changes |
|------|---------|
| `pkg/agentsdk/tool_caps.go` | Add `WriteTool` interface |
| `internal/agent/stream_tool_exec.go` | Add barrier tracking, `DispatchBarrier`, `DrainBarriers` |
| `internal/agent/agent.go` | Wire `isBarrier` check into `finalizeTool` |
| `internal/agent/stream_tool_exec_test.go` | Add barrier tests |

## Risks

- **Complexity**: The barrier logic adds state machine complexity to the executor
- **Performance**: Queued safe tools lose the streaming overlap benefit
- **Deadlock**: If a barrier tool panics, queued tools may never execute — need panic recovery

## Acceptance Criteria

- [ ] `write_file` followed by `read_file` in the same response executes in correct order
- [ ] `read_file` before `write_file` still dispatches during streaming
- [ ] Multiple barriers are processed sequentially
- [ ] All existing streaming executor tests pass
- [ ] New barrier tests cover edge cases
