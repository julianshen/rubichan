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

// ToArchived converts a Turn to ArchivedTurn for serialization
func (t *Turn) ToArchived() ArchivedTurn {
	return ArchivedTurn{
		ID:            t.ID,
		AssistantText: t.AssistantText,
		ThinkingText:  t.ThinkingText,
		ToolCalls:     t.ToolCalls,
		Status:        t.Status,
		ErrorMsg:      t.ErrorMsg,
		StartTime:     t.StartTime,
		ArchivedAt:    time.Now(),
	}
}

// ToTurn converts an ArchivedTurn back to Turn
func (a *ArchivedTurn) ToTurn() *Turn {
	return &Turn{
		ID:            a.ID,
		AssistantText: a.AssistantText,
		ThinkingText:  a.ThinkingText,
		ToolCalls:     a.ToolCalls,
		Status:        a.Status,
		ErrorMsg:      a.ErrorMsg,
		StartTime:     a.StartTime,
	}
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

// ArchiveOldTurns moves turns before index to disk.
// Splits into two phases to avoid lock contention:
// - Fast phase (locked): copy Turn objects to buffer
// - Slow phase (unlocked): marshal and write to disk
// This prevents rendering threads from blocking during I/O.
func (c *TurnCache) ArchiveOldTurns(beforeIndex int) error {
	// Fast phase: copy turns to buffer while locked
	type archiveJob struct {
		index   int
		archive ArchivedTurn
	}
	var jobs []archiveJob

	c.mu.Lock()
	for i := 0; i < beforeIndex; i++ {
		// Skip if already archived
		if _, alreadyArchived := c.archivedPaths[i]; alreadyArchived {
			continue
		}

		turn, ok := c.turns[i]
		if !ok {
			continue // Turn not in memory, skip
		}

		// Convert to ArchivedTurn (still locked, minimal work)
		jobs = append(jobs, archiveJob{index: i, archive: turn.ToArchived()})
	}
	c.mu.Unlock()

	// Slow phase: marshal and write to disk (unlocked)
	archiveSessionDir := filepath.Join(c.archiveDir, c.sessionID)
	if err := os.MkdirAll(archiveSessionDir, 0755); err != nil {
		return fmt.Errorf("create archive dir: %w", err)
	}

	for _, job := range jobs {
		data, err := json.MarshalIndent(job.archive, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal archive: %w", err)
		}
		archivePath := filepath.Join(archiveSessionDir, fmt.Sprintf("turn-%d.json", job.index))
		if err := os.WriteFile(archivePath, data, 0644); err != nil {
			return fmt.Errorf("write archive: %w", err)
		}

		// Track archived path and remove from memory (locked)
		c.mu.Lock()
		c.archivedPaths[job.index] = archivePath
		delete(c.turns, job.index)
		c.mu.Unlock()
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

	// Unmarshal and convert back to Turn
	var archived ArchivedTurn
	if err := json.Unmarshal(data, &archived); err != nil {
		return nil, fmt.Errorf("unmarshal archive: %w", err)
	}

	return archived.ToTurn(), nil
}
