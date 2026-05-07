# Cached Microcompact (cache_edits API) Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port Claude Code's cached microcompact to rubichan. Uses Anthropic's `cache_edits` / `cache_reference` API to remove tool results from the conversation without invalidating the cached prefix, dramatically reducing context window usage.

**Architecture:** Track tool results by `tool_use_id`. When microcompact triggers, queue deletions as `cache_edits` blocks inserted into the last user message. Add `cache_reference` to tool_result blocks within the cached prefix. Pinned edits are re-sent at original positions for cache hits.

**Tech Stack:** Go, Anthropic provider, `pkg/agentsdk` types.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `pkg/agentsdk/cache_edit.go` | `CacheEdit`, `CacheReference` types for SDK |
| `internal/agent/cached_microcompact.go` | `CachedMicrocompactService`, edit tracking, application |
| `internal/agent/cached_microcompact_test.go` | Tests for edit queuing, application, cache hit/miss |
| `internal/provider/anthropic/transformer.go` | Serialize cache_edits and cache_reference blocks |

---

## Chunk 1: SDK Types

### Task 1: Define CacheEdit types

**Files:**
- Create: `pkg/agentsdk/cache_edit.go`

**Code:**

```go
package agentsdk

// CacheEditType represents the type of cache edit operation.
type CacheEditType string

const (
	CacheEditDelete CacheEditType = "delete"
)

// CacheEdit represents an edit to remove content from the cached prefix.
type CacheEdit struct {
	Type            CacheEditType `json:"type"`
	CacheReference  string        `json:"cache_reference"`
}

// CacheReference marks a block as referenceable for cache edits.
type CacheReference struct {
	Type string `json:"type"` // "cache_reference"
	ID   string `json:"id"`   // references tool_use_id
}
```

**Test:**

```go
package agentsdk

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCacheEditJSON(t *testing.T) {
	edit := CacheEdit{Type: CacheEditDelete, CacheReference: "tu_01"}
	data, err := json.Marshal(edit)
	require.NoError(t, err)
	require.Contains(t, string(data), `"type":"delete"`)
	require.Contains(t, string(data), `"cache_reference":"tu_01"`)
}

func TestCacheReferenceJSON(t *testing.T) {
	ref := CacheReference{Type: "cache_reference", ID: "tu_01"}
	data, err := json.Marshal(ref)
	require.NoError(t, err)
	require.Contains(t, string(data), `"type":"cache_reference"`)
	require.Contains(t, string(data), `"id":"tu_01"`)
}
```

**Command:**
```bash
go test ./pkg/agentsdk/... -run TestCacheEdit -v
```

**Expected:** PASS.

---

## Chunk 2: Cached Microcompact Service

### Task 2: Implement CachedMicrocompactService

**Files:**
- Create: `internal/agent/cached_microcompact.go`

**Code:**

```go
package agent

import (
	"sync"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// CachedMicrocompactService uses Anthropic's cache_edits API to remove tool
// results without invalidating the cached prefix.
type CachedMicrocompactService struct {
	mu          sync.Mutex
	pendingEdits []agentsdk.CacheEdit
	enabled     bool
}

// NewCachedMicrocompactService creates a new service.
func NewCachedMicrocompactService(enabled bool) *CachedMicrocompactService {
	return &CachedMicrocompactService{
		enabled: enabled,
	}
}

// IsEnabled returns whether the service is enabled.
func (s *CachedMicrocompactService) IsEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabled
}

// QueueEdit queues a deletion for a tool result by its tool_use_id.
func (s *CachedMicrocompactService) QueueEdit(toolUseID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.enabled {
		return
	}
	s.pendingEdits = append(s.pendingEdits, agentsdk.CacheEdit{
		Type:           agentsdk.CacheEditDelete,
		CacheReference: toolUseID,
	})
}

// TakeEdits returns and clears all pending edits.
func (s *CachedMicrocompactService) TakeEdits() []agentsdk.CacheEdit {
	s.mu.Lock()
	defer s.mu.Unlock()
	edits := s.pendingEdits
	s.pendingEdits = nil
	return edits
}

// HasEdits returns true if there are pending edits.
func (s *CachedMicrocompactService) HasEdits() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pendingEdits) > 0
}

// Reset clears all pending edits.
func (s *CachedMicrocompactService) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingEdits = nil
}
```

**Test:**

```go
package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCachedMicrocompactQueueAndTake(t *testing.T) {
	s := NewCachedMicrocompactService(true)
	s.QueueEdit("tu_01")
	s.QueueEdit("tu_02")

	require.True(t, s.HasEdits())
	edits := s.TakeEdits()
	require.Len(t, edits, 2)
	require.Equal(t, "tu_01", edits[0].CacheReference)
	require.False(t, s.HasEdits())
}

func TestCachedMicrocompactDisabled(t *testing.T) {
	s := NewCachedMicrocompactService(false)
	s.QueueEdit("tu_01")
	require.False(t, s.HasEdits())
}

func TestCachedMicrocompactReset(t *testing.T) {
	s := NewCachedMicrocompactService(true)
	s.QueueEdit("tu_01")
	s.Reset()
	require.False(t, s.HasEdits())
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestCachedMicrocompact -v
```

**Expected:** All tests PASS.

---

## Chunk 3: Anthropic Transformer Support

### Task 3: Add cache_edits serialization

**Files:**
- Modify: `internal/provider/anthropic/transformer.go`

Add types:
```go
type apiCacheEdit struct {
	Type           string `json:"type"`            // "delete"
	CacheReference string `json:"cache_reference"` // tool_use_id
}

type apiCacheEditsBlock struct {
	Type  string         `json:"type"` // "cache_edits"
	Edits []apiCacheEdit `json:"edits"`
}
```

Modify message conversion to support cache_edits blocks in user messages:
```go
// In convertContentBlocks, handle cache_edits type:
func convertContentBlocks(blocks []provider.ContentBlock) []apiContentBlock {
	var out []apiContentBlock
	for _, b := range blocks {
		// ... existing handling ...
		if b.Type == "cache_edits" {
			// Serialize as cache_edits block
			out = append(out, apiContentBlock{Type: "cache_edits"})
			continue
		}
		// ...
	}
	return out
}
```

**Note:** Full implementation requires extending `ContentBlock` in `pkg/agentsdk` to carry `CacheEdits` field, then serializing appropriately.

---

## Validation Commands

```bash
go test ./pkg/agentsdk/...
go test ./internal/agent/...
go test ./internal/provider/anthropic/...
golangci-lint run ./internal/agent/... ./internal/provider/anthropic/...
gofmt -l .
```

---

## PR Description

**Title:** `[BEHAVIORAL] Cached microcompact via Anthropic cache_edits API`

**Body:**
- `CachedMicrocompactService` queues deletions of old tool results
- Uses Anthropic `cache_edits` / `cache_reference` API
- Removes tool results without invalidating cached prefix
- `QueueEdit(toolUseID)` to mark a tool result for deletion
- `TakeEdits()` returns and clears pending edits for serialization
- SDK types: `CacheEdit`, `CacheReference`
- Transformer support for serializing cache_edits blocks
- Ports Claude Code's `microCompact.ts` cached MC path to Go

**Commit prefix:** `[BEHAVIORAL]`
