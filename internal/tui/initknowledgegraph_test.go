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
	assert.NotNil(t, overlay.form)
	assert.Nil(t, overlay.Result())
}

func TestInitKnowledgeGraphOverlay_ViewRendersForm(t *testing.T) {
	overlay := NewInitKnowledgeGraphOverlay(80, 24)
	view := overlay.View()
	// Form should render with project name field visible
	assert.NotEmpty(t, view)
}

func TestInitKnowledgeGraphOverlay_DoneInitiallyFalse(t *testing.T) {
	overlay := NewInitKnowledgeGraphOverlay(80, 24)
	assert.False(t, overlay.Done())
}

func TestInitKnowledgeGraphOverlay_EscapeKeyCancels(t *testing.T) {
	overlay := NewInitKnowledgeGraphOverlay(80, 24)
	updated, _ := overlay.Update(tea.KeyMsg{Type: tea.KeyEscape})
	assert.True(t, updated.Done())
	assert.Nil(t, updated.Result())
}

func TestInitKnowledgeGraphOverlay_WindowResizeUpdatesSize(t *testing.T) {
	overlay := NewInitKnowledgeGraphOverlay(80, 24)
	updated, _ := overlay.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	updatedOverlay := updated.(*InitKnowledgeGraphOverlay)
	assert.Equal(t, 120, updatedOverlay.width)
	assert.Equal(t, 40, updatedOverlay.height)
}

func TestParseCommaSeparated(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"scaling, monitoring", []string{"scaling", "monitoring"}},
		{"", []string{}},
		{"single", []string{"single"}},
		{"a, b, c", []string{"a", "b", "c"}},
	}

	for _, test := range tests {
		result := parseCommaSeparated(test.input)
		assert.Equal(t, test.expected, result)
	}
}
