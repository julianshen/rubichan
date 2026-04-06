// internal/tui/turncache.go
package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ArchivedTurn is the serialized format for archived turns
type ArchivedTurn struct {
	ID            string             `json:"id"`
	AssistantText string             `json:"assistant_text"`
	ThinkingText  string             `json:"thinking_text"`
	ToolCalls     []RenderedToolCall `json:"tool_calls"`
	Status        string             `json:"status"`
	ErrorMsg      string             `json:"error_msg"`
	StartTime     time.Time          `json:"start_time"`
	ArchivedAt    time.Time          `json:"archived_at"`
}

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

// GetTurn gets a turn by index, checking memory first then archive
func (c *TurnCache) GetTurn(index int) (*Turn, error) {
	c.mu.RLock()
	if turn, ok := c.turns[index]; ok {
		c.mu.RUnlock()
		return turn, nil
	}
	c.mu.RUnlock()

	// Try loading from archive
	return c.LoadFromArchive(index)
}

// ArchiveOldTurns moves turns before index to disk
func (c *TurnCache) ArchiveOldTurns(beforeIndex int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Create archive directory
	archiveSessionDir := filepath.Join(c.archiveDir, c.sessionID)
	if err := os.MkdirAll(archiveSessionDir, 0755); err != nil {
		return fmt.Errorf("create archive dir: %w", err)
	}

	// Archive turns before threshold
	for i := 0; i < beforeIndex; i++ {
		if turn, ok := c.turns[i]; ok {
			// Convert to ArchivedTurn
			archived := ArchivedTurn{
				ID:            turn.ID,
				AssistantText: turn.AssistantText,
				ThinkingText:  turn.ThinkingText,
				ToolCalls:     turn.ToolCalls,
				Status:        turn.Status,
				ErrorMsg:      turn.ErrorMsg,
				StartTime:     turn.StartTime,
				ArchivedAt:    time.Now(),
			}

			// Write to disk
			data, err := json.MarshalIndent(archived, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal archive: %w", err)
			}
			archivePath := filepath.Join(archiveSessionDir, fmt.Sprintf("turn-%d.json", i))
			if err := os.WriteFile(archivePath, data, 0644); err != nil {
				return fmt.Errorf("write archive: %w", err)
			}

			// Track archived path
			c.archivedPaths[i] = archivePath

			// Remove from memory
			delete(c.turns, i)
		}
	}

	return nil
}

// LoadFromArchive loads a turn from archived disk storage
func (c *TurnCache) LoadFromArchive(index int) (*Turn, error) {
	c.mu.RLock()
	archivePath, ok := c.archivedPaths[index]
	c.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("turn %d not archived", index)
	}

	// Read from disk
	data, err := os.ReadFile(archivePath)
	if err != nil {
		return nil, fmt.Errorf("read archive: %w", err)
	}

	// Unmarshal
	var archived ArchivedTurn
	if err := json.Unmarshal(data, &archived); err != nil {
		return nil, fmt.Errorf("unmarshal archive: %w", err)
	}

	// Convert back to Turn
	turn := &Turn{
		ID:            archived.ID,
		AssistantText: archived.AssistantText,
		ThinkingText:  archived.ThinkingText,
		ToolCalls:     archived.ToolCalls,
		Status:        archived.Status,
		ErrorMsg:      archived.ErrorMsg,
		StartTime:     archived.StartTime,
	}

	return turn, nil
}
