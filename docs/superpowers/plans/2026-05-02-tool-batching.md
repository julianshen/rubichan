# Tool Batching Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the current streaming executor's simple parallel dispatch with intelligent batching that groups adjacent concurrency-safe tools and runs safe batches in parallel while unsafe batches run sequentially.

**Architecture:** Port Claude Code's `partitionToolCalls` algorithm to Go. A `ToolBatch` struct groups consecutive tool calls with the same concurrency safety. A `BatchExecutor` runs safe batches with a semaphore-bound worker pool and unsafe batches sequentially. The existing `streamingToolExecutor` barrier pattern is preserved — batching applies to the post-stream `executeTools` phase.

**Tech Stack:** Go, existing `agentsdk.Tool` interfaces, `golang.org/x/sync/semaphore` (or channel-based semaphore as already used).

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/toolexec/batch.go` | `ToolBatch`, `partitionToolCalls`, `BatchExecutor` |
| `internal/toolexec/batch_test.go` | Unit tests for partitioning and execution |
| `internal/agent/agent.go` | Wire `BatchExecutor` into `executeTools` |

---

## Chunk 1: Partition Logic

### Task 1: Define ToolBatch and partitionToolCalls

**Files:**
- Create: `internal/toolexec/batch.go`
- Test: `internal/toolexec/batch_test.go`

- [ ] **Step 1: Write the failing test**

```go
package toolexec

import (
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

func TestPartitionToolCalls(t *testing.T) {
	// Mock tools: safe tools return true for IsConcurrencySafe,
	// unsafe tools return false.
	safe := &mockTool{name: "read", safe: true}
	unsafe := &mockTool{name: "write", safe: false}
	lookup := &mockLookup{
		tools: map[string]agentsdk.Tool{
			"read":  safe,
			"write": unsafe,
		},
	}

	calls := []ToolCall{
		{Name: "read", Input: []byte(`{}`)},
		{Name: "read", Input: []byte(`{}`)},
		{Name: "write", Input: []byte(`{}`)},
		{Name: "read", Input: []byte(`{}`)},
	}

	batches := partitionToolCalls(lookup, calls)
	if len(batches) != 3 {
		t.Fatalf("expected 3 batches, got %d", len(batches))
	}
	if !batches[0].IsConcurrent {
		t.Error("batch 0 should be concurrent")
	}
	if len(batches[0].Calls) != 2 {
		t.Errorf("batch 0 should have 2 calls, got %d", len(batches[0].Calls))
	}
	if batches[1].IsConcurrent {
		t.Error("batch 1 should be sequential")
	}
	if batches[2].IsConcurrent {
		t.Error("batch 2 should be sequential (single unsafe breaks the chain)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/toolexec/ -run TestPartitionToolCalls -v`
Expected: FAIL — `partitionToolCalls` undefined

- [ ] **Step 3: Write minimal implementation**

```go
package toolexec

import (
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// ToolBatch groups consecutive tool calls with the same concurrency safety.
type ToolBatch struct {
	IsConcurrent bool
	Calls        []ToolCall
}

// partitionToolCalls groups adjacent tool calls into batches.
// Consecutive concurrency-safe tools share a batch; the first unsafe
// tool breaks the batch and starts a new sequential batch.
func partitionToolCalls(lookup ToolLookup, calls []ToolCall) []ToolBatch {
	if len(calls) == 0 {
		return nil
	}

	var batches []ToolBatch
	var current ToolBatch

	for _, tc := range calls {
		tool, ok := lookup.Get(tc.Name)
		if !ok {
			// Unknown tools are treated as unsafe (fail-closed).
			tool = nil
		}

		isSafe := isConcurrencySafe(tool, tc.Input)

		if len(current.Calls) == 0 {
			current.IsConcurrent = isSafe
			current.Calls = append(current.Calls, tc)
			continue
		}

		if current.IsConcurrent == isSafe {
			// Same safety level — extend current batch.
			current.Calls = append(current.Calls, tc)
		} else {
			// Safety changed — finalize current and start new.
			batches = append(batches, current)
			current = ToolBatch{IsConcurrent: isSafe, Calls: []ToolCall{tc}}
		}
	}

	if len(current.Calls) > 0 {
		batches = append(batches, current)
	}
	return batches
}

// isConcurrencySafe checks whether a tool is safe to run in parallel
// with other tools. Falls back to false for unknown tools or tools
// that don't implement the marker interface.
//
// The cheaper ConcurrencySafeTool check comes first to avoid JSON
// parsing for tools that don't need per-invocation discrimination.
func isConcurrencySafe(tool agentsdk.Tool, input json.RawMessage) bool {
	if tool == nil {
		return false
	}
	// Fast path: static concurrency safety (no JSON parsing).
	if cs, ok := tool.(agentsdk.ConcurrencySafeTool); ok {
		// If the tool also implements InputConcurrencySafeTool, the
		// per-invocation check takes precedence — but only after we
		// know the tool participates in the concurrency safety protocol.
		if ics, ok := tool.(agentsdk.InputConcurrencySafeTool); ok {
			var parsed map[string]interface{}
			if err := json.Unmarshal(input, &parsed); err != nil {
				// Malformed input: fail-closed (treat as unsafe).
				return false
			}
			return ics.IsConcurrencySafe(parsed)
		}
		return cs.IsConcurrencySafe()
	}
	return false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/toolexec/ -run TestPartitionToolCalls -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/toolexec/batch.go internal/toolexec/batch_test.go
git commit -m "[STRUCTURAL] Add tool batch partitioning logic"
```

---

## Chunk 2: Batch Executor

### Task 2: Implement BatchExecutor

**Files:**
- Modify: `internal/toolexec/batch.go`
- Test: `internal/toolexec/batch_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestBatchExecutor(t *testing.T) {
	// safe tool sleeps 50ms, unsafe sleeps 100ms
	safe := &mockTool{name: "safe", safe: true, delay: 50 * time.Millisecond}
	unsafe := &mockTool{name: "unsafe", safe: false, delay: 100 * time.Millisecond}
	lookup := &mockLookup{tools: map[string]agentsdk.Tool{"safe": safe, "unsafe": unsafe}}

	calls := []ToolCall{
		{Name: "safe"},
		{Name: "safe"},
		{Name: "unsafe"},
	}

	exec := NewBatchExecutor(lookup, RegistryExecutor(lookup), 10)
	start := time.Now()
	results := exec.Execute(context.Background(), calls)
	elapsed := time.Since(start)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// Two safe tools in parallel should take ~50ms, then unsafe ~100ms.
	// Total should be < 200ms (sequential would be 200ms).
	if elapsed > 180*time.Millisecond {
		t.Errorf("expected parallel execution, took %v", elapsed)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/toolexec/ -run TestBatchExecutor -v`
Expected: FAIL — `BatchExecutor` undefined

- [ ] **Step 3: Write minimal implementation**

```go
// defaultMaxParallel is the default concurrency limit for tool batch
// execution. Matches Claude Code's MAX_TOOL_USE_CONCURRENCY=10.
const defaultMaxParallel = 10

// BatchExecutor runs tool calls in batches, parallelizing safe batches
// and serializing unsafe batches.
type BatchExecutor struct {
	lookup      ToolLookup
	handler     HandlerFunc
	maxParallel int
}

// NewBatchExecutor creates a batch executor with the given lookup,
// handler, and max parallelism. maxParallel <= 0 defaults to 10.
func NewBatchExecutor(lookup ToolLookup, handler HandlerFunc, maxParallel int) *BatchExecutor {
	if maxParallel <= 0 {
		maxParallel = defaultMaxParallel
	}
	return &BatchExecutor{
		lookup:      lookup,
		handler:     handler,
		maxParallel: maxParallel,
	}
}

// Execute runs all tool calls and returns results in call order.
func (be *BatchExecutor) Execute(ctx context.Context, calls []ToolCall) []Result {
	batches := partitionToolCalls(be.lookup, calls)
	results := make([]Result, 0, len(calls))

	for _, batch := range batches {
		if batch.IsConcurrent {
			results = append(results, be.executeConcurrently(ctx, batch.Calls)...)
		} else {
			results = append(results, be.executeSerially(ctx, batch.Calls)...)
		}
	}
	return results
}

func (be *BatchExecutor) executeConcurrently(ctx context.Context, calls []ToolCall) []Result {
	sem := make(chan struct{}, be.maxParallel)
	var wg sync.WaitGroup
	results := make([]Result, len(calls))

	for i, tc := range calls {
		wg.Add(1)
		go func(idx int, call ToolCall) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[idx] = Result{Content: ctx.Err().Error(), IsError: true}
				return
			}
			defer func() { <-sem }()
			results[idx] = be.handler(ctx, call)
		}(i, tc)
	}
	wg.Wait()
	return results
}

func (be *BatchExecutor) executeSerially(ctx context.Context, calls []ToolCall) []Result {
	results := make([]Result, 0, len(calls))
	for _, tc := range calls {
		if ctx.Err() != nil {
			results = append(results, Result{Content: ctx.Err().Error(), IsError: true})
			continue
		}
		results = append(results, be.handler(ctx, tc))
	}
	return results
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/toolexec/ -run TestBatchExecutor -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/toolexec/batch.go internal/toolexec/batch_test.go
git commit -m "[BEHAVIORAL] Add BatchExecutor with concurrent/serial batch execution"
```

---

## Chunk 3: Wire into Agent

### Task 3: Replace executeTools with BatchExecutor

**Files:**
- Modify: `internal/agent/agent.go` (find `executeTools` or equivalent)
- Test: `internal/agent/agent_test.go`

- [ ] **Step 1: Find the current tool execution path**

Search for where tools are executed after streaming in `internal/agent/agent.go`.
Look for: `executeTools`, `executeSingleTool`, tool result collection.

- [ ] **Step 2: Write integration test**

```go
func TestExecuteTools_Batching(t *testing.T) {
	// Verify that two safe tools run in parallel and an unsafe tool runs
	// after them sequentially.
	// Use the existing agent test infrastructure.
}
```

- [ ] **Step 3: Wire BatchExecutor into executeTools**

Replace the current sequential or simple-parallel execution with `BatchExecutor`.
Preserve existing behavior: approval checks, result capping, offloading, verdict evaluation.

The key change is in the post-stream execution path (after `Drain()` returns):
```go
// Before: sequential for-loop over pendingTools
// After:
batchExec := toolexec.NewBatchExecutor(a.registry, a.makeHandler(), maxParallelTools)
results := batchExec.Execute(ctx, pendingTools)
```

- [ ] **Step 4: Run agent tests**

Run: `go test ./internal/agent/... -count=1`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/agent.go internal/agent/agent_test.go
git commit -m "[BEHAVIORAL] Wire BatchExecutor into agent tool execution"
```

---

## Chunk 4: Validation

### Task 4: Run full validation

- [ ] **Step 1: Run all tests**

```bash
go test ./...
```
Expected: All PASS

- [ ] **Step 2: Run linter**

```bash
golangci-lint run ./internal/toolexec/... ./internal/agent/...
```
Expected: Clean

- [ ] **Step 3: Check formatting**

```bash
gofmt -l internal/toolexec/batch.go internal/toolexec/batch_test.go
```
Expected: No output (clean)

- [ ] **Step 4: Commit if any fixes needed**

```bash
git commit -m "[STRUCTURAL] Fix lint/format issues from BatchExecutor"
```

---

## Appendix: Mock Types for Tests

```go
type mockTool struct {
	name   string
	safe   bool
	delay  time.Duration
}

func (m *mockTool) Name() string { return m.name }
func (m *mockTool) Description() string { return "" }
func (m *mockTool) InputSchema() json.RawMessage { return []byte(`{}`) }
func (m *mockTool) Execute(ctx context.Context, input json.RawMessage) (agentsdk.ToolResult, error) {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	return agentsdk.ToolResult{Content: m.name}, nil
}
func (m *mockTool) IsConcurrencySafe() bool { return m.safe }

type mockLookup struct {
	tools map[string]agentsdk.Tool
}

func (m *mockLookup) Get(name string) (agentsdk.Tool, bool) {
	t, ok := m.tools[name]
	return t, ok
}
```
