// internal/tui/turnwindow_test.go
package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"
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

	if window.visibleStart < 0 {
		t.Errorf("visibleStart should be >= 0, got %d", window.visibleStart)
	}
	if window.visibleEnd < window.visibleStart {
		t.Errorf("visibleEnd should be >= visibleStart, got start=%d end=%d", window.visibleStart, window.visibleEnd)
	}
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
			Status:        "done",
		}
		window.AddTurn(i, turn)
	}

	window.UpdateVisibleRange(0, 10)
	output, err := window.RenderVisible(renderer)
	if err != nil {
		t.Fatalf("RenderVisible failed: %v", err)
	}

	if output == "" {
		t.Errorf("Output should not be empty")
	}

	// Verify that output contains some of the turn content
	if !strings.Contains(output, "Turn") {
		t.Errorf("Output should contain turn content, got: %s", output)
	}
}
