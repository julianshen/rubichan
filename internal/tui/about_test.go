package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAboutOverlay_ImplementsOverlay(t *testing.T) {
	// Compile-time interface check.
	var _ Overlay = (*AboutOverlay)(nil)
}

func TestNewAboutOverlay(t *testing.T) {
	a := NewAboutOverlay(80, 24)
	require.NotNil(t, a)
	assert.False(t, a.cancelled)
	assert.Equal(t, 80, a.width)
	assert.Equal(t, 24, a.height)
	assert.NotNil(t, a.viewport)
}

func TestAboutOverlay_ViewContainsLogo(t *testing.T) {
	a := NewAboutOverlay(80, 24)
	view := a.View()
	assert.Contains(t, view, "Rubichan")
	assert.Contains(t, view, "何が好き")
}

func TestAboutOverlay_DoneInitiallyFalse(t *testing.T) {
	a := NewAboutOverlay(80, 24)
	assert.False(t, a.Done())
}

func TestAboutOverlay_ResultIsNil(t *testing.T) {
	a := NewAboutOverlay(80, 24)
	assert.Nil(t, a.Result())
}

func TestAboutOverlay_EscapeKeyClosesOverlay(t *testing.T) {
	a := NewAboutOverlay(80, 24)
	newA, _ := a.Update(tea.KeyMsg{Type: tea.KeyEscape})
	a = newA.(*AboutOverlay)
	assert.True(t, a.Done())
	assert.Nil(t, a.Result())
}

func TestAboutOverlay_QKeyClosesOverlay(t *testing.T) {
	a := NewAboutOverlay(80, 24)
	newA, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	a = newA.(*AboutOverlay)
	assert.True(t, a.Done())
}

func TestAboutOverlay_CapitalQKeyClosesOverlay(t *testing.T) {
	a := NewAboutOverlay(80, 24)
	newA, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Q'}})
	a = newA.(*AboutOverlay)
	assert.True(t, a.Done())
}

func TestAboutOverlay_UpDownNavigation(t *testing.T) {
	a := NewAboutOverlay(80, 24)
	initialOffset := a.viewport.YOffset

	// Scroll down.
	newA, _ := a.Update(tea.KeyMsg{Type: tea.KeyDown})
	a = newA.(*AboutOverlay)
	assert.Greater(t, a.viewport.YOffset, initialOffset)

	// Scroll up.
	newA, _ = a.Update(tea.KeyMsg{Type: tea.KeyUp})
	a = newA.(*AboutOverlay)
	assert.Equal(t, initialOffset, a.viewport.YOffset)
}

func TestAboutOverlay_HomeEndNavigation(t *testing.T) {
	a := NewAboutOverlay(80, 24)

	// Go to bottom.
	newA, _ := a.Update(tea.KeyMsg{Type: tea.KeyEnd})
	a = newA.(*AboutOverlay)
	bottomOffset := a.viewport.YOffset

	// Go to top.
	newA, _ = a.Update(tea.KeyMsg{Type: tea.KeyHome})
	a = newA.(*AboutOverlay)
	assert.Equal(t, 0, a.viewport.YOffset)

	// Confirm we were at bottom (offset should be > 0).
	assert.Greater(t, bottomOffset, 0)
}

func TestAboutOverlay_PageNavigation(t *testing.T) {
	a := NewAboutOverlay(80, 24)
	initialOffset := a.viewport.YOffset

	// Page down.
	newA, _ := a.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	a = newA.(*AboutOverlay)
	assert.Greater(t, a.viewport.YOffset, initialOffset)

	// Page up (back to top).
	newA, _ = a.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	a = newA.(*AboutOverlay)
	assert.Equal(t, 0, a.viewport.YOffset)
}

func TestAboutOverlay_WindowResizeUpdatesViewport(t *testing.T) {
	a := NewAboutOverlay(80, 24)
	oldWidth := a.viewport.Width
	oldHeight := a.viewport.Height

	// Resize to larger dimensions.
	newA, _ := a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	a = newA.(*AboutOverlay)

	assert.Equal(t, 120, a.width)
	assert.Equal(t, 40, a.height)
	assert.Greater(t, a.viewport.Width, oldWidth)
	assert.Greater(t, a.viewport.Height, oldHeight)
}

func TestAboutOverlay_UnhandledKeyDoesntClose(t *testing.T) {
	a := NewAboutOverlay(80, 24)
	newA, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	a = newA.(*AboutOverlay)
	assert.False(t, a.Done())
}
