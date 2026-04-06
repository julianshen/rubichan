// internal/tui/turncache_test.go
package tui

import (
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
