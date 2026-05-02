# Result Storage for Large Outputs

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist oversized tool results (>50KB) to disk, replacing them in messages with references + previews. Enforce a per-message budget (200KB default) by iteratively persisting largest results.

**Architecture:** Port ccgo's `execution/result_storage.go:1-286`. A `ResultStorage` manages disk persistence. `BudgetEnforcer` checks aggregate size and offloads largest results first.

**Tech Stack:** Go, existing `ResultBudgetEnforcer`, `os`/`io` for disk I/O.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/toolexec/result_storage.go` | `ResultStorage`, `persistResult`, `loadResult` |
| `internal/toolexec/result_storage_test.go` | Tests for persistence and budget enforcement |
| `internal/agent/budget_enforcer.go` | Wire disk offloading into budget enforcement |

---

## Chunk 1: Result Storage

### Task 1: Implement ResultStorage with disk persistence

**Files:**
- Create: `internal/toolexec/result_storage.go`

**Code:**

```go
package toolexec

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	
	"github.com/google/uuid"
)

const (
	// defaultOversizedThreshold is the byte size above which a result
	// is persisted to disk instead of kept in memory.
	defaultOversizedThreshold = 50 * 1024 // 50KB
	
	// defaultBudgetPerMessage is the total byte budget for all tool
	// results in a single message.
	defaultBudgetPerMessage = 200 * 1024 // 200KB
)

// ResultStorage manages disk persistence for oversized tool results.
type ResultStorage struct {
	mu        sync.RWMutex
	dir       string
	threshold int
	refs      map[string]string // toolUseID -> file path
}

// NewResultStorage creates a storage manager in the given directory.
func NewResultStorage(dir string) (*ResultStorage, error) {
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("create result storage dir: %w", err)
	}
	
	return &ResultStorage{
		dir:       dir,
		threshold: defaultOversizedThreshold,
		refs:      make(map[string]string),
	}, nil
}

// Store persists a result if it exceeds the threshold. Returns a
// reference string and a preview (first N chars) for the message.
func (rs *ResultStorage) Store(toolUseID string, content string) (ref string, preview string, stored bool, err error) {
	if len(content) <= rs.threshold {
		return "", content, false, nil
	}
	
	path := filepath.Join(rs.dir, fmt.Sprintf("%s_%s.txt", toolUseID, uuid.New().String()[:8]))
	if err := os.WriteFile(path, []byte(content), 0640); err != nil {
		return "", "", false, fmt.Errorf("write result: %w", err)
	}
	
	rs.mu.Lock()
	rs.refs[toolUseID] = path
	rs.mu.Unlock()
	
	previewLen := 500
	if len(content) < previewLen {
		previewLen = len(content)
	}
	
	ref = fmt.Sprintf("result://%s", filepath.Base(path))
	preview = fmt.Sprintf("%s... [full result stored at %s, %d bytes]",
		content[:previewLen], ref, len(content))
	
	return ref, preview, true, nil
}

// Load retrieves a persisted result by reference.
func (rs *ResultStorage) Load(ref string) (string, error) {
	// Extract filename from result:// URL
	filename := filepath.Base(ref)
	path := filepath.Join(rs.dir, filename)
	
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("load result: %w", err)
	}
	
	return string(data), nil
}

// Cleanup removes all stored results.
func (rs *ResultStorage) Cleanup() error {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	
	for _, path := range rs.refs {
		_ = os.Remove(path)
	}
	rs.refs = make(map[string]string)
	
	return nil
}
```

**Test:**

```go
func TestResultStorage(t *testing.T) {
	dir := t.TempDir()
	rs, err := NewResultStorage(dir)
	require.NoError(t, err)
	
	// Small result — not stored
	ref, preview, stored, err := rs.Store("call-1", "small")
	require.NoError(t, err)
	require.False(t, stored)
	require.Equal(t, "small", preview)
	
	// Large result — stored
	large := strings.Repeat("x", 60*1024)
	ref, preview, stored, err = rs.Store("call-2", large)
	require.NoError(t, err)
	require.True(t, stored)
	require.Contains(t, preview, "result://")
	
	// Load back
	loaded, err := rs.Load(ref)
	require.NoError(t, err)
	require.Equal(t, large, loaded)
}
```

**Command:**
```bash
go test ./internal/toolexec/... -run TestResultStorage -v
```

**Expected:** Test fails — storage not yet wired.

---

## Chunk 2: Budget Enforcement with Disk Offloading

### Task 2: Wire ResultStorage into BudgetEnforcer

**Files:**
- Modify: `internal/agent/budget_enforcer.go`

**Code:**

```go
type ResultBudgetEnforcer struct {
	// ... existing fields ...
	
	// storage offloads oversized results to disk.
	storage *toolexec.ResultStorage
}

// Enforce now offloads largest results first until budget is met.
func (be *ResultBudgetEnforcer) Enforce(results []toolExecResult) ([]toolExecResult, error) {
	total := be.totalSize(results)
	if total <= be.budget {
		return results, nil
	}
	
	// Sort by size descending
	sorted := make([]*toolExecResult, len(results))
	for i := range results {
		sorted[i] = &results[i]
	}
	sort.Slice(sorted, func(i, j int) bool {
		return len(sorted[i].content) > len(sorted[j].content)
	})
	
	// Offload largest results until budget is met
	for _, res := range sorted {
		if total <= be.budget {
			break
		}
		
		ref, preview, stored, err := be.storage.Store(res.toolUseID, res.content)
		if err != nil {
			return nil, fmt.Errorf("store result: %w", err)
		}
		if stored {
			res.content = preview
			res.isStored = true
			res.storageRef = ref
			total = be.totalSize(results)
		}
	}
	
	return results, nil
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestResultBudgetEnforcer -v
```

**Expected:** Existing tests pass + new storage tests pass.

---

## Validation Commands

```bash
go test ./internal/toolexec/...
go test ./internal/agent/...
go test -cover ./internal/agent/...
golangci-lint run ./internal/agent/...
gofmt -l .
```

---

## PR Description

**Title:** `[BEHAVIORAL] Result storage for large tool outputs`

**Body:**
- Persist tool results >50KB to disk, replace with reference + preview
- Enforce 200KB per-message budget by offloading largest results first
- `ResultStorage` manages disk I/O with cleanup
- Integrates with existing `ResultBudgetEnforcer`
- Ports ccgo's `execution/result_storage.go:1-286` pattern to Go

**Commit prefix:** `[BEHAVIORAL]`
