// internal/tui/turnwindow_test.go
package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTurnWindow_ComputesVisibleRange(t *testing.T) {
	cache := NewTurnCache(t.TempDir(), "test", 50)
	window := NewTurnWindow(cache)

	// Add 20 turns
	for i := 0; i < 20; i++ {
		turn := &Turn{ID: fmt.Sprintf("turn-%d", i)}
		window.AddTurn(i, turn)
	}

	// Simulate viewport at height 10 (showing ~5-10 turns)
	window.UpdateVisibleRange(0, 10)

	// Use public API to check visible range
	start, end := window.GetVisibleRange()
	assert.GreaterOrEqual(t, start, 0, "visibleStart should be >= 0")
	assert.GreaterOrEqual(t, end, start, "visibleEnd should be >= visibleStart")
}

func TestTurnWindow_RenderVisibleOnly(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewTurnCache(tmpDir, "test", 50)
	window := NewTurnWindow(cache)
	renderer := &TurnRenderer{}

	// Add 10 turns
	for i := 0; i < 10; i++ {
		turn := &Turn{
			ID:            fmt.Sprintf("turn-%d", i),
			AssistantText: fmt.Sprintf("Turn %d", i),
			StartTime:     time.Now(),
			Status:        TurnStatusDone,
		}
		window.AddTurn(i, turn)
	}

	window.UpdateVisibleRange(0, 10)
	output, err := window.RenderVisible(renderer)
	require.NoError(t, err, "RenderVisible should not fail")
	assert.NotEmpty(t, output, "Output should not be empty")
	assert.Contains(t, output, "Turn", "Output should contain turn content")
}
