package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHelpOverlay_ImplementsOverlay(t *testing.T) {
	// Compile-time interface check.
	var _ Overlay = (*HelpOverlay)(nil)
}

func TestNewHelpOverlay(t *testing.T) {
	h := NewHelpOverlay(80, 24)
	require.NotNil(t, h)
	assert.False(t, h.cancelled)
	assert.Equal(t, 80, h.width)
	assert.Equal(t, 24, h.height)
	assert.NotNil(t, h.viewport)
}

func TestHelpOverlay_ViewContainsCtrl(t *testing.T) {
	h := NewHelpOverlay(80, 24)
	view := h.View()
	assert.Contains(t, view, "Ctrl")
}

func TestHelpOverlay_DoneInitiallyFalse(t *testing.T) {
	h := NewHelpOverlay(80, 24)
	assert.False(t, h.Done())
}

func TestHelpOverlay_ResultIsNil(t *testing.T) {
	h := NewHelpOverlay(80, 24)
	assert.Nil(t, h.Result())
}

func TestHelpOverlay_EscapeKeyClosesOverlay(t *testing.T) {
	h := NewHelpOverlay(80, 24)
	newH, _ := h.Update(tea.KeyMsg{Type: tea.KeyEscape})
	h = newH.(*HelpOverlay)
	assert.True(t, h.Done())
	assert.Nil(t, h.Result())
}

func TestHelpOverlay_QKeyClosesOverlay(t *testing.T) {
	h := NewHelpOverlay(80, 24)
	newH, _ := h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	h = newH.(*HelpOverlay)
	assert.True(t, h.Done())
}

func TestHelpOverlay_CapitalQKeyClosesOverlay(t *testing.T) {
	h := NewHelpOverlay(80, 24)
	newH, _ := h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Q'}})
	h = newH.(*HelpOverlay)
	assert.True(t, h.Done())
}

func TestHelpOverlay_UpDownNavigation(t *testing.T) {
	h := NewHelpOverlay(80, 24)
	initialOffset := h.viewport.YOffset

	// Scroll down.
	newH, _ := h.Update(tea.KeyMsg{Type: tea.KeyDown})
	h = newH.(*HelpOverlay)
	assert.Greater(t, h.viewport.YOffset, initialOffset)

	// Scroll up.
	newH, _ = h.Update(tea.KeyMsg{Type: tea.KeyUp})
	h = newH.(*HelpOverlay)
	assert.Equal(t, initialOffset, h.viewport.YOffset)
}

func TestHelpOverlay_HomeEndNavigation(t *testing.T) {
	h := NewHelpOverlay(80, 24)

	// Go to bottom.
	newH, _ := h.Update(tea.KeyMsg{Type: tea.KeyEnd})
	h = newH.(*HelpOverlay)
	bottomOffset := h.viewport.YOffset

	// Go to top.
	newH, _ = h.Update(tea.KeyMsg{Type: tea.KeyHome})
	h = newH.(*HelpOverlay)
	assert.Equal(t, 0, h.viewport.YOffset)

	// Confirm we were at bottom (offset should be > 0).
	assert.Greater(t, bottomOffset, 0)
}

func TestHelpOverlay_PageNavigation(t *testing.T) {
	h := NewHelpOverlay(80, 24)
	initialOffset := h.viewport.YOffset

	// Page down.
	newH, _ := h.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	h = newH.(*HelpOverlay)
	assert.Greater(t, h.viewport.YOffset, initialOffset)

	// Page up (back to top).
	newH, _ = h.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	h = newH.(*HelpOverlay)
	assert.Equal(t, 0, h.viewport.YOffset)
}

func TestHelpOverlay_WindowResizeUpdatesViewport(t *testing.T) {
	h := NewHelpOverlay(80, 24)
	oldWidth := h.viewport.Width
	oldHeight := h.viewport.Height

	// Resize to larger dimensions.
	newH, _ := h.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	h = newH.(*HelpOverlay)

	assert.Equal(t, 120, h.width)
	assert.Equal(t, 40, h.height)
	assert.Greater(t, h.viewport.Width, oldWidth)
	assert.Greater(t, h.viewport.Height, oldHeight)
}

func TestHelpOverlay_UnhandledKeyDoesntClosing(t *testing.T) {
	h := NewHelpOverlay(80, 24)
	newH, _ := h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	h = newH.(*HelpOverlay)
	assert.False(t, h.Done())
}
