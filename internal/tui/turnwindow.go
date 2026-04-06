// internal/tui/turnwindow.go
package tui

import (
	"sync"
)

// TurnWindow manages memory-efficient access to turns.
type TurnWindow struct {
	cache        *TurnCache
	visibleStart int
	visibleEnd   int
	mu           sync.RWMutex
}

// NewTurnWindow creates a new turn window.
func NewTurnWindow(cache *TurnCache) *TurnWindow {
	return &TurnWindow{
		cache: cache,
	}
}

// AddTurn adds a turn to the window.
func (w *TurnWindow) AddTurn(index int, turn *Turn) {
	w.cache.AddTurn(index, turn)
}

// UpdateVisibleRange updates which turns are visible.
func (w *TurnWindow) UpdateVisibleRange(scrollPos, viewportHeight int) {
	// TODO: compute visible range based on scroll position
}

// RenderVisible renders only visible turns.
func (w *TurnWindow) RenderVisible(renderer *TurnRenderer) (string, error) {
	// TODO: render visible turns
	return "", nil
}
