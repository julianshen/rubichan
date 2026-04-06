// internal/tui/turncache_test.go
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTurnCache_Initialize(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "test-session-1"

	cache := NewTurnCache(tmpDir, sessionID, 50)

	assert.NotNil(t, cache)
	assert.Equal(t, 50, cache.maxMemoryTurns)
	assert.Equal(t, tmpDir, cache.archiveDir)
	assert.Equal(t, sessionID, cache.sessionID)
}

func TestTurnCache_AddAndGet(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewTurnCache(tmpDir, "test-session", 50)

	turn := &Turn{
		ID:            "turn-1",
		AssistantText: "Hello",
		Status:        "done",
	}

	cache.AddTurn(1, turn)

	retrieved, err := cache.GetTurn(1)
	assert.NoError(t, err)
	assert.NotNil(t, retrieved)
	assert.Equal(t, "turn-1", retrieved.ID)
	assert.Equal(t, "Hello", retrieved.AssistantText)
}

func TestTurnCache_GetNonexistent(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewTurnCache(tmpDir, "test-session", 50)

	_, err := cache.GetTurn(999)
	assert.Error(t, err)
}

func TestTurnWindow_Initialize(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewTurnCache(tmpDir, "test-session", 50)
	window := NewTurnWindow(cache)

	assert.NotNil(t, window)
	assert.NotNil(t, window.cache)
}

func TestTurnWindow_AddTurn(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewTurnCache(tmpDir, "test-session", 50)
	window := NewTurnWindow(cache)

	turn := &Turn{
		ID:            "turn-1",
		AssistantText: "Hello",
		Status:        "done",
	}

	window.AddTurn(1, turn)

	// Should be able to retrieve from underlying cache
	retrieved, err := cache.GetTurn(1)
	assert.NoError(t, err)
	assert.Equal(t, "turn-1", retrieved.ID)
}

func TestTurnCache_ArchiveOldTurns(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewTurnCache(tmpDir, "test-1", 2) // keep 2 in memory

	// Add 5 turns
	for i := 0; i < 5; i++ {
		turn := &Turn{
			ID:            fmt.Sprintf("turn-%d", i),
			AssistantText: fmt.Sprintf("Turn %d", i),
			Status:        "done",
		}
		cache.AddTurn(i, turn)
	}

	// Archive turns before index 3 (should archive 0, 1, 2)
	err := cache.ArchiveOldTurns(3)
	assert.NoError(t, err)

	// Verify archive files exist
	for i := 0; i < 3; i++ {
		archivePath := filepath.Join(tmpDir, "test-1", fmt.Sprintf("turn-%d.json", i))
		_, err := os.Stat(archivePath)
		assert.NoError(t, err, "Archive file for turn %d not found at %s", i, archivePath)
	}
}

func TestTurnCache_LoadFromArchive(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewTurnCache(tmpDir, "test-1", 2)

	// Create and archive a turn
	originalTurn := &Turn{
		ID:            "turn-0",
		AssistantText: "Original text",
		Status:        "done",
	}
	cache.AddTurn(0, originalTurn)
	err := cache.ArchiveOldTurns(1)
	assert.NoError(t, err)

	// Remove from memory to simulate eviction
	cache.mu.Lock()
	delete(cache.turns, 0)
	cache.mu.Unlock()

	// Load from archive
	loaded, err := cache.LoadFromArchive(0)
	assert.NoError(t, err)
	assert.NotNil(t, loaded)
	assert.Equal(t, "turn-0", loaded.ID)
	assert.Equal(t, "Original text", loaded.AssistantText)
}
