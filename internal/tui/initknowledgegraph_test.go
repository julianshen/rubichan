package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitKnowledgeGraphOverlay_ImplementsOverlay(t *testing.T) {
	overlay := NewInitKnowledgeGraphOverlay(80, 24)
	var _ Overlay = overlay
}

func TestNewInitKnowledgeGraphOverlay(t *testing.T) {
	overlay := NewInitKnowledgeGraphOverlay(80, 24)
	require.NotNil(t, overlay)
	assert.Equal(t, 80, overlay.width)
	assert.Equal(t, 24, overlay.height)
	assert.False(t, overlay.cancelled)
}

func TestInitKnowledgeGraphOverlay_ViewContainsContent(t *testing.T) {
	overlay := NewInitKnowledgeGraphOverlay(80, 24)
	view := overlay.View()
	assert.Contains(t, view, "Initialize Knowledge Graph")
	assert.Contains(t, view, "questionnaire")
	assert.Contains(t, view, "codebase")
}

func TestInitKnowledgeGraphOverlay_DoneInitiallyFalse(t *testing.T) {
	overlay := NewInitKnowledgeGraphOverlay(80, 24)
	assert.False(t, overlay.Done())
}

func TestInitKnowledgeGraphOverlay_ResultIsNil(t *testing.T) {
	overlay := NewInitKnowledgeGraphOverlay(80, 24)
	assert.Nil(t, overlay.Result())
}

func TestInitKnowledgeGraphOverlay_EscapeKeyClosesOverlay(t *testing.T) {
	overlay := NewInitKnowledgeGraphOverlay(80, 24)
	updated, _ := overlay.Update(tea.KeyMsg{Type: tea.KeyEscape})
	assert.True(t, updated.Done())
}

func TestInitKnowledgeGraphOverlay_QKeyClosesOverlay(t *testing.T) {
	overlay := NewInitKnowledgeGraphOverlay(80, 24)
	updated, _ := overlay.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	assert.True(t, updated.Done())
}

func TestInitKnowledgeGraphOverlay_CapitalQKeyClosesOverlay(t *testing.T) {
	overlay := NewInitKnowledgeGraphOverlay(80, 24)
	updated, _ := overlay.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Q'}})
	assert.True(t, updated.Done())
}

func TestInitKnowledgeGraphOverlay_NKeyClosesOverlay(t *testing.T) {
	overlay := NewInitKnowledgeGraphOverlay(80, 24)
	updated, _ := overlay.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	assert.True(t, updated.Done())
}

func TestInitKnowledgeGraphOverlay_YKeyClosesOverlay(t *testing.T) {
	overlay := NewInitKnowledgeGraphOverlay(80, 24)
	updated, _ := overlay.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	assert.True(t, updated.Done())
}

func TestInitKnowledgeGraphOverlay_WindowResizeUpdatesSize(t *testing.T) {
	overlay := NewInitKnowledgeGraphOverlay(80, 24)
	updated, _ := overlay.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	updatedOverlay := updated.(*InitKnowledgeGraphOverlay)
	assert.Equal(t, 120, updatedOverlay.width)
	assert.Equal(t, 40, updatedOverlay.height)
}

func TestInitKnowledgeGraphOverlay_UnhandledKeyDoesntClose(t *testing.T) {
	overlay := NewInitKnowledgeGraphOverlay(80, 24)
	updated, _ := overlay.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	assert.False(t, updated.Done())
}
