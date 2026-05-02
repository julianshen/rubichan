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
