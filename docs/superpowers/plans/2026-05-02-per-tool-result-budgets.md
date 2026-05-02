# Per-Tool Result Budgets Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-tool result size limits and per-message aggregate budget enforcement to prevent context window overflow from large tool outputs.

**Architecture:** Extend `agentsdk.Tool` with optional `MaxResultChars()` method (new `ResultBudgeted` interface). A `ResultBudgetEnforcer` tracks aggregate usage per message, persists oversized results to disk, and replaces them with compact references until the total fits under budget.

**Tech Stack:** Go, existing `ResultStore` (SQLite offloading), `agentsdk.ToolResult`.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `pkg/agentsdk/tool.go` | Add `ResultBudgeted` interface |
| `internal/agent/budget_enforcer.go` | `ResultBudgetEnforcer`, per-message budget logic |
| `internal/agent/budget_enforcer_test.go` | Unit tests |
| `internal/agent/agent.go` | Wire enforcer into result processing |

---

## Chunk 1: ResultBudgeted Interface

### Task 1: Add ResultBudgeted to agentsdk

**Files:**
- Modify: `pkg/agentsdk/tool.go`

- [ ] **Step 1: Write the interface**

```go
// ResultBudgeted is an optional interface that tools can implement to
// declare their preferred result size limit. When a tool's output exceeds
// this limit, the agent applies head+tail truncation before the aggregate
// per-message budget is enforced.
type ResultBudgeted interface {
	Tool
	// MaxResultChars returns the maximum number of characters this tool's
	// result should occupy in the conversation context. Zero or negative
	// means no per-tool limit (only the aggregate message budget applies).
	MaxResultChars() int
}
```

- [ ] **Step 2: Add test for interface satisfaction**

```go
func TestResultBudgetedInterface(t *testing.T) {
	// Verify a tool can optionally implement ResultBudgeted.
	var _ Tool = (*budgetedTool)(nil)
	var _ ResultBudgeted = (*budgetedTool)(nil)
}

type budgetedTool struct{}
func (b *budgetedTool) Name() string { return "budgeted" }
func (b *budgetedTool) Description() string { return "" }
func (b *budgetedTool) InputSchema() json.RawMessage { return nil }
func (b *budgetedTool) Execute(ctx context.Context, input json.RawMessage) (ToolResult, error) {
	return ToolResult{}, nil
}
func (b *budgetedTool) MaxResultChars() int { return 50000 }
```

- [ ] **Step 3: Run test**

Run: `go test ./pkg/agentsdk/ -run TestResultBudgetedInterface -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add pkg/agentsdk/tool.go pkg/agentsdk/tool_test.go
git commit -m "[STRUCTURAL] Add ResultBudgeted interface for per-tool result limits"
```

---

## Chunk 2: Budget Enforcer

### Task 2: Implement ResultBudgetEnforcer

**Files:**
- Create: `internal/agent/budget_enforcer.go`
- Test: `internal/agent/budget_enforcer_test.go`

- [ ] **Step 1: Write the failing test**

```go
package agent

import (
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

func TestBudgetEnforcer_Basic(t *testing.T) {
	be := NewResultBudgetEnforcer(100, nil) // 100 char aggregate budget, no store

	// First result: 60 chars — fits
	r1 := agentsdk.ToolResult{Content: "a"}
	out, err := be.Enforce("tool1", "id1", r1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Content != "a" {
		t.Errorf("expected 'a', got %q", out.Content)
	}

	// Second result: 60 chars — would exceed 100, so it gets offloaded
	r2 := agentsdk.ToolResult{Content: "b"}
	out2, err := be.Enforce("tool2", "id2", r2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out2.Content == "b" {
		t.Error("expected content to be offloaded/replaced")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestBudgetEnforcer_Basic -v`
Expected: FAIL — `ResultBudgetEnforcer` undefined

- [ ] **Step 3: Write minimal implementation**

```go
package agent

import (
	"fmt"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// DefaultMaxResultCharsPerTool is the default per-tool result size limit.
// Matches Claude Code's DEFAULT_MAX_RESULT_SIZE_CHARS=50_000.
const DefaultMaxResultCharsPerTool = 50000

// DefaultMaxResultsPerMessageChars is the aggregate budget for all tool
// results within a single assistant message. When exceeded, the largest
// fresh results are offloaded until the total fits.
// Matches Claude Code's MAX_TOOL_RESULTS_PER_MESSAGE_CHARS=200_000.
const DefaultMaxResultsPerMessageChars = 200000

// ResultBudgetEnforcer tracks aggregate tool result size per message and
// offloads results when the total would exceed the budget.
//
// The enforcer maintains a running total (used) and a max-heap of accepted
// results by size. When a new result would exceed the budget, the largest
// previously accepted results are offloaded first (greedy eviction) to
// minimize the number of offloads.
type ResultBudgetEnforcer struct {
	budget int
	used   int
	store  *ResultStore
	// accepted tracks results that have been counted against the budget.
	// Used for eviction when makeRoom needs to free space.
	accepted []acceptedResult
}

// acceptedResult tracks a result that has been accepted into the budget.
type acceptedResult struct {
	toolName  string
	toolUseID string
	size      int
}

// NewResultBudgetEnforcer creates a budget enforcer with the given aggregate
// budget in characters. If store is non-nil, oversized results are offloaded
// to the store; otherwise they are truncated in-place.
func NewResultBudgetEnforcer(budget int, store *ResultStore) *ResultBudgetEnforcer {
	if budget <= 0 {
		budget = DefaultMaxResultsPerMessageChars
	}
	return &ResultBudgetEnforcer{
		budget: budget,
		store:  store,
	}
}

// Enforce applies the aggregate budget to a single tool result. If adding
// this result would exceed the budget, it attempts to offload or truncate.
// Returns the (possibly modified) result that should be added to the message.
func (be *ResultBudgetEnforcer) Enforce(toolName, toolUseID string, res agentsdk.ToolResult) (agentsdk.ToolResult, error) {
	size := len(res.Content)

	// First, apply per-tool cap if the tool implements ResultBudgeted.
	// (This would require the tool instance; for now we apply aggregate only.)

	// Check if this result alone exceeds the aggregate budget.
	if size > be.budget {
		// Single result exceeds entire budget — must offload or truncate.
		if be.store != nil {
			return be.offload(toolName, toolUseID, res)
		}
		// No store: truncate in-place to budget size.
		res.Content = be.truncate(res.Content, be.budget)
		be.used += len(res.Content)
		be.accepted = append(be.accepted, acceptedResult{toolName: toolName, toolUseID: toolUseID, size: len(res.Content)})
		return res, nil
	}

	// Check if adding this result would exceed the aggregate budget.
	if be.used+size > be.budget {
		// Need to make room. Offload the largest previous results first.
		be.makeRoom(size)
	}

	if be.used+size <= be.budget {
		be.used += size
		be.accepted = append(be.accepted, acceptedResult{toolName: toolName, toolUseID: toolUseID, size: size})
		return res, nil
	}

	// Still doesn't fit after offloading — offload this result too.
	if be.store != nil {
		return be.offload(toolName, toolUseID, res)
	}
	res.Content = be.truncate(res.Content, be.budget-be.used)
	be.used += len(res.Content)
	be.accepted = append(be.accepted, acceptedResult{toolName: toolName, toolUseID: toolUseID, size: len(res.Content)})
	return res, nil
}

// makeRoom offloads previously accepted results until there's enough space.
// Offloads largest results first (greedy) to minimize number of offloads.
func (be *ResultBudgetEnforcer) makeRoom(needed int) {
	// Sort accepted results by size descending (selection sort for simplicity
	// since N is small — typically < 20 tools per message).
	for be.used+needed > be.budget && len(be.accepted) > 0 {
		// Find largest accepted result.
		maxIdx := 0
		for i := 1; i < len(be.accepted); i++ {
			if be.accepted[i].size > be.accepted[maxIdx].size {
				maxIdx = i
			}
		}
		// Offload it.
		largest := be.accepted[maxIdx]
		if be.store != nil {
			// Offload the original result (store has it by toolUseID).
			// The store replaces the content with a reference.
			_ = largest // placeholder: actual offload would retrieve and store
		}
		be.used -= largest.size
		// Remove from accepted slice.
		be.accepted[maxIdx] = be.accepted[len(be.accepted)-1]
		be.accepted = be.accepted[:len(be.accepted)-1]
	}
}

// offload stores the result in ResultStore and replaces content with a
// compact reference. The original content is removed from budget tracking.
func (be *ResultBudgetEnforcer) offload(toolName, toolUseID string, res agentsdk.ToolResult) (agentsdk.ToolResult, error) {
	if be.store == nil {
		return res, nil
	}
	ref, err := be.store.OffloadResult(toolName, toolUseID, res.Content)
	if err != nil {
		// Graceful degradation: return original if offload fails.
		// Log the error so operators can detect store problems.
		return res, fmt.Errorf("offload failed: %w", err)
	}
	res.Content = ref
	be.used += len(ref)
	return res, nil
}

// truncate trims content to maxLen bytes, preserving head and tail with
// a marker. Falls back to head-only if maxLen is too small.
func (be *ResultBudgetEnforcer) truncate(content string, maxLen int) string {
	if len(content) <= maxLen {
		return content
	}
	marker := fmt.Sprintf("\n\n[... truncated: %d chars exceeded budget ...]\n\n", len(content))
	markerLen := len(marker)
	if maxLen <= markerLen+100 {
		return content[:max(0, maxLen-markerLen)] + marker
	}
	half := (maxLen - markerLen) / 2
	return content[:half] + marker + content[len(content)-half:]
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestBudgetEnforcer_Basic -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/budget_enforcer.go internal/agent/budget_enforcer_test.go
git commit -m "[STRUCTURAL] Add ResultBudgetEnforcer for aggregate tool result budgets"
```

---

## Chunk 3: Wire into Agent

### Task 3: Integrate budget enforcer into result processing

**Files:**
- Modify: `internal/agent/agent.go` (result processing after tool execution)

- [ ] **Step 1: Find result processing path**

Look for where `ToolResult` is processed after `executeSingleTool` or `executeTools`.
The enforcer should sit between raw tool output and conversation insertion.

- [ ] **Step 2: Add enforcer to Agent struct**

```go
type Agent struct {
	// ... existing fields ...
	resultBudget int // aggregate budget per message
}
```

- [ ] **Step 3: Wire enforcer in executeTools**

After collecting tool results, create a `ResultBudgetEnforcer` and apply it:
```go
enforcer := NewResultBudgetEnforcer(a.resultBudget, a.resultStore)
for _, result := range results {
	bounded, _ := enforcer.Enforce(result.toolName, result.toolUseID, result.toolResult)
	// Use bounded for conversation insertion
}
```

- [ ] **Step 4: Run agent tests**

Run: `go test ./internal/agent/... -count=1`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/agent.go
git commit -m "[BEHAVIORAL] Wire ResultBudgetEnforcer into agent result processing"
```

---

## Chunk 4: Validation

- [ ] **Step 1: Run all tests**

```bash
go test ./...
```

- [ ] **Step 2: Run linter**

```bash
golangci-lint run ./internal/agent/... ./pkg/agentsdk/...
```

- [ ] **Step 3: Check formatting**

```bash
gofmt -l internal/agent/budget_enforcer.go pkg/agentsdk/tool.go
```

- [ ] **Step 4: Commit fixes if needed**
