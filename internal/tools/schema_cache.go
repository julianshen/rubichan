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
	key      string // hash of name+description+schema
	rendered []byte // cached JSON representation
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
	return append([]byte(nil), entry.rendered...)
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
	return fmt.Sprintf("%x", h.Sum(nil))
}
