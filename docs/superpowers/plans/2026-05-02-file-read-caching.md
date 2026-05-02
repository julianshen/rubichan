# File Read Caching Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cache file read results to avoid redundant I/O when the same file is read multiple times in a session. Cache invalidation uses mtime + size comparison.

**Architecture:** Add a `FileReadCache` to `ToolUseContext` (or agent state). The cache stores `FileStateInfo` (mtime, size, content) keyed by absolute path. `FileReadTool` checks the cache before reading; on hit, returns cached content if mtime/size match. On miss or staleness, reads file and updates cache.

**Tech Stack:** Go, existing `internal/tools/file.go` (FileReadTool), `os.Stat` for mtime/size.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/tools/file_cache.go` | `FileReadCache`, `FileStateInfo`, cache operations |
| `internal/tools/file_cache_test.go` | Unit tests |
| `internal/tools/file.go` | Integrate cache into FileReadTool |

---

## Chunk 1: Cache Types

### Task 1: Define FileReadCache and FileStateInfo

**Files:**
- Create: `internal/tools/file_cache.go`
- Test: `internal/tools/file_cache_test.go`

- [ ] **Step 1: Write the failing test**

```go
package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileReadCache_Basic(t *testing.T) {
	cache := NewFileReadCache()

	// Create a temp file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(tmpFile, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	// First read: cache miss
	content, hit := cache.Get(tmpFile)
	if hit {
		t.Error("expected cache miss on first read")
	}

	// Store in cache
	info, err := os.Stat(tmpFile)
	if err != nil {
		t.Fatal(err)
	}
	cache.Put(tmpFile, info, "hello")

	// Second read: cache hit
	content, hit = cache.Get(tmpFile)
	if !hit {
		t.Error("expected cache hit")
	}
	if content != "hello" {
		t.Errorf("expected 'hello', got %q", content)
	}

	// Modify file: cache should detect staleness
	os.WriteFile(tmpFile, []byte("world"), 0644)
	_, hit = cache.Get(tmpFile)
	if hit {
		t.Error("expected cache miss after file modification")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/ -run TestFileReadCache_Basic -v`
Expected: FAIL — `FileReadCache` undefined

- [ ] **Step 3: Write minimal implementation**

```go
package tools

import (
	"os"
	"sync"
	"time"
)

// FileStateInfo captures the file metadata and content at read time.
// Used to detect staleness on subsequent reads.
type FileStateInfo struct {
	MTime   time.Time
	Size    int64
	Content string
}

// FileReadCache caches file read results keyed by absolute path.
// Thread-safe for concurrent access. No eviction — intended for
// sessions with a bounded number of file reads (typically < 1000).
// If unbounded growth becomes a concern, add an LRU eviction policy.
type FileReadCache struct {
	mu    sync.RWMutex
	state map[string]FileStateInfo
}

// NewFileReadCache creates an empty file read cache.
func NewFileReadCache() *FileReadCache {
	return &FileReadCache{
		state: make(map[string]FileStateInfo),
	}
}

// Get checks the cache for a file. Returns (content, true) if the cached
// entry matches the current file's mtime and size. Returns ("", false) on
// miss or staleness.
//
// Uses time.Time.Equal() instead of != to avoid false staleness from
// filesystems with sub-second precision differences.
func (c *FileReadCache) Get(path string) (string, bool) {
	c.mu.RLock()
	cached, ok := c.state[path]
	c.mu.RUnlock()
	if !ok {
		return "", false
	}

	info, err := os.Stat(path)
	if err != nil {
		// File disappeared or unreadable — treat as stale.
		return "", false
	}

	if !info.ModTime().Equal(cached.MTime) || info.Size() != cached.Size {
		return "", false
	}

	return cached.Content, true
}

// Put stores a file's content and metadata in the cache.
func (c *FileReadCache) Put(path string, info os.FileInfo, content string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state[path] = FileStateInfo{
		MTime:   info.ModTime(),
		Size:    info.Size(),
		Content: content,
	}
}

// Invalidate removes a path from the cache. Called after writes/edits
// to ensure subsequent reads don't return stale data.
func (c *FileReadCache) Invalidate(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.state, path)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/ -run TestFileReadCache_Basic -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tools/file_cache.go internal/tools/file_cache_test.go
git commit -m "[STRUCTURAL] Add FileReadCache with mtime/size invalidation"
```

---

## Chunk 2: Integrate into FileReadTool

### Task 2: Wire cache into FileReadTool

**Files:**
- Modify: `internal/tools/file.go`

- [ ] **Step 1: Find FileReadTool implementation**

Read `internal/tools/file.go` to locate `FileReadTool` and its `Execute` method.

- [ ] **Step 2: Add cache field to FileReadTool**

```go
type FileReadTool struct {
	// ... existing fields ...
	cache *FileReadCache
}
```

- [ ] **Step 3: Modify Execute to check cache**

In the `Execute` method, before reading the file:
```go
// Check cache first.
if ft.cache != nil {
	if content, hit := ft.cache.Get(absPath); hit {
		return agentsdk.ToolResult{
			Content:        content,
			DisplayContent: content,
		}, nil
	}
}

// Read file...
content, err := os.ReadFile(absPath)
// ...

// Update cache.
if ft.cache != nil {
	info, _ := os.Stat(absPath)
	if info != nil {
		ft.cache.Put(absPath, info, string(content))
	}
}
```

- [ ] **Step 4: Invalidate cache on write/edit**

In `FileWriteTool` and `FileEditTool` (or `PatchFileTool`), after successful modification:
```go
if ft.cache != nil {
	ft.cache.Invalidate(absPath)
}
```

- [ ] **Step 5: Run tool tests**

Run: `go test ./internal/tools/... -count=1`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/tools/file.go
git commit -m "[BEHAVIORAL] Integrate FileReadCache into FileReadTool"
```

---

## Chunk 3: Agent Integration

### Task 3: Pass cache to tools

**Files:**
- Modify: `internal/agent/agent.go` or tool initialization

- [ ] **Step 1: Create shared cache in Agent**

```go
type Agent struct {
	// ... existing fields ...
	fileCache *tools.FileReadCache
}
```

- [ ] **Step 2: Initialize cache and pass to FileReadTool**

In `New` or tool registration:
```go
a.fileCache = tools.NewFileReadCache()
// When creating FileReadTool:
fileTool := &tools.FileReadTool{Cache: a.fileCache}
```

- [ ] **Step 3: Run agent tests**

Run: `go test ./internal/agent/... -count=1`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agent/agent.go
git commit -m "[BEHAVIORAL] Wire FileReadCache into agent tool initialization"
```

---

## Chunk 4: Validation

- [ ] **Step 1: Run all tests**

```bash
go test ./...
```

- [ ] **Step 2: Run linter**

```bash
golangci-lint run ./internal/tools/... ./internal/agent/...
```

- [ ] **Step 3: Check formatting**

```bash
gofmt -l internal/tools/file_cache.go internal/tools/file.go
```

- [ ] **Step 4: Commit fixes if needed**
