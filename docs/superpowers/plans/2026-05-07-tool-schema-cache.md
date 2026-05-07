# Tool Schema Cache Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port Claude Code's tool schema cache to rubichan. A `ToolSchemaCache` prevents re-rendering tool definitions when the tool registry hasn't changed, preserving prompt cache stability.

**Architecture:** Session-scoped cache keyed by tool name + description + input schema hash. Cache invalidated when tool definitions change. Used by the Anthropic provider's transformer when serializing tool definitions.

**Tech Stack:** Go, existing `tools.Registry`, `internal/provider/anthropic/transformer.go`.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/tools/schema_cache.go` | `ToolSchemaCache` with hash-based invalidation |
| `internal/tools/schema_cache_test.go` | Tests for cache hit, miss, invalidation |
| `internal/provider/anthropic/transformer.go` | Use cache when building tool definitions |

---

## Chunk 1: Core Cache Implementation

### Task 1: Implement ToolSchemaCache

**Files:**
- Create: `internal/tools/schema_cache.go`

**Code:**

```go
package tools

import (
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// ToolSchemaCache prevents re-rendering tool definitions when the registry
// hasn't changed, preserving prompt cache stability.
type ToolSchemaCache struct {
	mu      sync.RWMutex
	entries map[string]cachedSchema
}

type cachedSchema struct {
	key        string // hash of name+description+schema
	rendered   []byte // cached JSON representation
}

// NewToolSchemaCache creates a new empty cache.
func NewToolSchemaCache() *ToolSchemaCache {
	return &ToolSchemaCache{
		entries: make(map[string]cachedSchema),
	}
}

// Get looks up a cached schema for a tool. Returns nil if not found or stale.
func (c *ToolSchemaCache) Get(tool agentsdk.ToolDef) []byte {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[tool.Name]
	if !ok {
		return nil
	}
	if entry.key != schemaKey(tool) {
		return nil // stale
	}
	return entry.rendered
}

// Set stores a rendered schema for a tool.
func (c *ToolSchemaCache) Set(tool agentsdk.ToolDef, rendered []byte) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[tool.Name] = cachedSchema{
		key:      schemaKey(tool),
		rendered: append([]byte(nil), rendered...),
	}
}

// Invalidate removes a tool from the cache.
func (c *ToolSchemaCache) Invalidate(name string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, name)
}

// Reset clears all cached entries.
func (c *ToolSchemaCache) Reset() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]cachedSchema)
}

func schemaKey(tool agentsdk.ToolDef) string {
	h := sha256.New()
	h.Write([]byte(tool.Name))
	h.Write([]byte(tool.Description))
	h.Write(tool.InputSchema)
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}
```

**Test:**

```go
package tools

import (
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/require"
)

func TestToolSchemaCacheHit(t *testing.T) {
	c := NewToolSchemaCache()
	tool := agentsdk.ToolDef{Name: "read", Description: "Read files", InputSchema: []byte(`{}`)}

	c.Set(tool, []byte(`cached`))
	got := c.Get(tool)
	require.Equal(t, []byte(`cached`), got)
}

func TestToolSchemaCacheMiss(t *testing.T) {
	c := NewToolSchemaCache()
	tool := agentsdk.ToolDef{Name: "read", Description: "Read files", InputSchema: []byte(`{}`)}

	got := c.Get(tool)
	require.Nil(t, got)
}

func TestToolSchemaCacheStale(t *testing.T) {
	c := NewToolSchemaCache()
	tool := agentsdk.ToolDef{Name: "read", Description: "Read files", InputSchema: []byte(`{}`)}
	c.Set(tool, []byte(`cached`))

	// Change description → key changes → stale
	tool.Description = "Read files and images"
	got := c.Get(tool)
	require.Nil(t, got)
}

func TestToolSchemaCacheInvalidate(t *testing.T) {
	c := NewToolSchemaCache()
	tool := agentsdk.ToolDef{Name: "read", Description: "Read files", InputSchema: []byte(`{}`)}
	c.Set(tool, []byte(`cached`))

	c.Invalidate("read")
	got := c.Get(tool)
	require.Nil(t, got)
}

func TestToolSchemaCacheReset(t *testing.T) {
	c := NewToolSchemaCache()
	tool := agentsdk.ToolDef{Name: "read", Description: "Read files", InputSchema: []byte(`{}`)}
	c.Set(tool, []byte(`cached`))

	c.Reset()
	got := c.Get(tool)
	require.Nil(t, got)
}

func TestToolSchemaCacheNilSafe(t *testing.T) {
	var c *ToolSchemaCache
	_ = c.Get(agentsdk.ToolDef{})
	c.Set(agentsdk.ToolDef{}, nil)
	c.Invalidate("")
	c.Reset()
}
```

**Command:**
```bash
go test ./internal/tools/... -run TestToolSchemaCache -v
```

**Expected:** All tests PASS.

---

## Chunk 2: Integration with Anthropic Transformer

### Task 2: Use cache in transformer

**Files:**
- Modify: `internal/provider/anthropic/transformer.go`

Add field to Transformer:
```go
type Transformer struct {
	SchemaCache *tools.ToolSchemaCache
}
```

Modify tool conversion to use cache:
```go
// In ToProviderJSON, replace tool conversion loop:
for _, tool := range req.Tools {
	var rendered []byte
	if t.SchemaCache != nil {
		rendered = t.SchemaCache.Get(tool)
	}
	if rendered == nil {
		// Build and cache
		apiTool := apiTool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		}
		var err error
		rendered, err = json.Marshal(apiTool)
		if err != nil {
			return nil, err
		}
		if t.SchemaCache != nil {
			t.SchemaCache.Set(tool, rendered)
		}
	}
	// ... append to apiReq.Tools
}
```

**Note:** The actual implementation needs to integrate with the existing `apiTool` struct and `CacheControl` assignment. The cache stores the full rendered JSON including `cache_control`.

**Test:**

```go
func TestTransformerWithSchemaCache(t *testing.T) {
	cache := tools.NewToolSchemaCache()
	tr := &Transformer{SchemaCache: cache}

	req := provider.CompletionRequest{
		Model: "claude-3",
		Tools: []provider.ToolDef{
			{Name: "file", Description: "Read", InputSchema: []byte(`{}`)},
		},
	}

	_, err := tr.ToProviderJSON(req)
	require.NoError(t, err)

	// Second call should use cache
	_, err = tr.ToProviderJSON(req)
	require.NoError(t, err)
}
```

**Command:**
```bash
go test ./internal/provider/anthropic/... -run TestTransformerWithSchemaCache -v
```

**Expected:** PASS.

---

## Validation Commands

```bash
go test ./internal/tools/...
go test ./internal/provider/anthropic/...
golangci-lint run ./internal/tools/... ./internal/provider/anthropic/...
gofmt -l .
```

---

## PR Description

**Title:** `[BEHAVIORAL] Tool schema cache for prompt cache stability`

**Body:**
- `ToolSchemaCache` prevents re-rendering tool definitions when registry unchanged
- Hash-based invalidation (name + description + input schema)
- Integrated into Anthropic `Transformer` for cached tool JSON serialization
- Thread-safe with RWMutex
- Nil-safe (no-op when disabled)
- Ports Claude Code's `toolSchemaCache.ts` pattern to Go

**Commit prefix:** `[BEHAVIORAL]`
