// internal/tui/turncache.go
package tui

import (
	"fmt"
	"sync"
)

// TurnCache manages turn storage with automatic archival.
type TurnCache struct {
	mu             sync.RWMutex
	turns          map[int]*Turn  // in-memory turns
	archivedPaths  map[int]string // archived turn paths
	maxMemoryTurns int            // keep last N turns in memory
	archiveDir     string         // directory for archived turns
	sessionID      string         // session identifier
}

// NewTurnCache creates a new turn cache.
func NewTurnCache(archiveDir, sessionID string, maxMemoryTurns int) *TurnCache {
	return &TurnCache{
		turns:          make(map[int]*Turn),
		archivedPaths:  make(map[int]string),
		maxMemoryTurns: maxMemoryTurns,
		archiveDir:     archiveDir,
		sessionID:      sessionID,
	}
}

// AddTurn adds a turn to cache (in-memory by default)
func (c *TurnCache) AddTurn(index int, turn *Turn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.turns[index] = turn
}

// GetTurn gets a turn by index
func (c *TurnCache) GetTurn(index int) (*Turn, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if turn, ok := c.turns[index]; ok {
		return turn, nil
	}

	// TODO: load from archive if not in memory
	return nil, fmt.Errorf("turn %d not found", index)
}

// ArchiveOldTurns moves turns before index to disk
func (c *TurnCache) ArchiveOldTurns(beforeIndex int) error {
	// TODO: implement archival
	return nil
}
