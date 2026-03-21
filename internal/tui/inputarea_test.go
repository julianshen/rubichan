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

// --- Auto-grow tests ---

func TestInputAreaAutoGrow_MultiLineIncreasesHeight(t *testing.T) {
	t.Parallel()
	ia := NewInputArea()
	ia.Init()
	// Set multi-line content (5 lines)
	ia.SetValue("line1\nline2\nline3\nline4\nline5")
	ia.autoGrow()
	assert.Equal(t, 5, ia.Height(), "height should grow to match 5 lines of content")
}

func TestInputAreaAutoGrow_ResetShrinksToMin(t *testing.T) {
	t.Parallel()
	ia := NewInputArea()
	ia.Init()
	ia.SetValue("line1\nline2\nline3\nline4\nline5")
	ia.autoGrow()
	assert.Equal(t, 5, ia.Height())

	ia.Reset()
	assert.Equal(t, inputMinHeight, ia.Height(), "Reset should shrink height back to inputMinHeight")
}

func TestInputAreaAutoGrow_CappedAtMax(t *testing.T) {
	t.Parallel()
	ia := NewInputArea()
	ia.Init()
	// Set content with more lines than inputMaxHeight (8)
	ia.SetValue("1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12")
	ia.autoGrow()
	assert.Equal(t, inputMaxHeight, ia.Height(), "height should be capped at inputMaxHeight")
}

func TestInputAreaAutoGrow_SingleLineStaysAtMin(t *testing.T) {
	t.Parallel()
	ia := NewInputArea()
	ia.Init()
	ia.SetValue("just one line")
	ia.autoGrow()
	assert.Equal(t, inputMinHeight, ia.Height(), "single-line content should stay at inputMinHeight")
}
