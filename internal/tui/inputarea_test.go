package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestInputAreaCreation(t *testing.T) {
	ia := NewInputArea()
	assert.NotNil(t, ia)
	assert.Equal(t, "", ia.Value())
}

func TestInputAreaSetAndGet(t *testing.T) {
	ia := NewInputArea()
	ia.SetValue("hello\nworld")
	assert.Equal(t, "hello\nworld", ia.Value())
}

func TestInputAreaReset(t *testing.T) {
	ia := NewInputArea()
	ia.SetValue("test")
	ia.Reset()
	assert.Equal(t, "", ia.Value())
}

func TestInputAreaInit(t *testing.T) {
	ia := NewInputArea()
	cmd := ia.Init()
	// Init should return a focus command (non-nil)
	assert.NotNil(t, cmd)
}

func TestInputAreaView(t *testing.T) {
	ia := NewInputArea()
	view := ia.View()
	// View should render a non-empty string (at least the placeholder)
	assert.NotEmpty(t, view)
}

func TestInputAreaUpdate(t *testing.T) {
	ia := NewInputArea()
	ia.Init()
	// Send a rune key to the textarea
	cmd := ia.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	_ = cmd
	assert.Equal(t, "a", ia.Value())
}

func TestInputAreaFocusBlur(t *testing.T) {
	ia := NewInputArea()
	// Focus should return a command
	cmd := ia.Focus()
	assert.NotNil(t, cmd)
	// Blur should not panic
	ia.Blur()
}

func TestInputAreaViewContainsPlaceholder(t *testing.T) {
	ia := NewInputArea()
	view := ia.View()
	assert.Contains(t, view, "Type a message")
}
