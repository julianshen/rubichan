// internal/tui/turnwindow.go
package tui

import (
	"context"
	"strings"
	"sync"
)

// TurnWindow manages memory-efficient access to turns.
type TurnWindow struct {
	cache        *TurnCache
	visibleStart int
	visibleEnd   int
	maxTurnIndex int // highest turn index seen
	width        int // viewport width in characters
	mu           sync.RWMutex
}

// NewTurnWindow creates a new turn window.
func NewTurnWindow(cache *TurnCache) *TurnWindow {
	return &TurnWindow{
		cache: cache,
	}
}

// AddTurn adds a turn to the window and updates max index tracking.
func (w *TurnWindow) AddTurn(index int, turn *Turn) {
	w.cache.AddTurn(index, turn)

	w.mu.Lock()
	if index > w.maxTurnIndex {
		w.maxTurnIndex = index
	}
	w.mu.Unlock()
}

// GetVisibleRange returns the currently visible start and end indices (for testing).
func (w *TurnWindow) GetVisibleRange() (int, int) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.visibleStart, w.visibleEnd
}

// UpdateVisibleRange updates which turns are visible.
// Estimate lines-per-turn to calculate visible range with lookahead for smooth scrolling.
func (w *TurnWindow) UpdateVisibleRange(scrollPos, viewportHeight, viewportWidth int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.width = viewportWidth

	// Estimate turns per viewport (roughly 30 chars per line, accounting for wrapping)
	linesPerTurn := 5 // rough estimate
	turnsPerViewport := viewportHeight / linesPerTurn
	if turnsPerViewport < 1 {
		turnsPerViewport = 1
	}

	// Compute visible range based on scroll position
	// (This is simplified; real implementation would track pixel offsets)
	w.visibleStart = (scrollPos / linesPerTurn) - turnsPerViewport
	if w.visibleStart < 0 {
		w.visibleStart = 0
	}
	w.visibleEnd = w.visibleStart + (turnsPerViewport * 2) // slightly ahead for smoothness
}

// RenderVisible renders only visible turns.
// O(k) rendering where k = visible turn count (typically 5-10).
// Bounds-checks against maxTurnIndex to avoid expensive failed disk lookups.
func (w *TurnWindow) RenderVisible(renderer *TurnRenderer) (string, error) {
	w.mu.RLock()
	visibleStart := w.visibleStart
	visibleEnd := w.visibleEnd
	maxTurnIndex := w.maxTurnIndex
	width := w.width
	if width == 0 {
		width = 80 // default if not set
	}
	w.mu.RUnlock()

	// Bounds-check visibleEnd against actual turns
	if visibleEnd > maxTurnIndex {
		visibleEnd = maxTurnIndex
	}

	var output strings.Builder

	// Render visible turns only
	for i := visibleStart; i <= visibleEnd; i++ {
		turn, err := w.cache.GetTurn(i)
		if err != nil {
			// Turn not in range, continue to next
			continue
		}

		rendered, err := renderer.Render(context.Background(), turn, RenderOptions{Width: width})
		if err != nil {
			continue
		}

		output.WriteString(rendered)
		output.WriteString("\n")
	}

	return output.String(), nil
}
