package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelTextInputOverlayImplementsOverlay(t *testing.T) {
	overlay, _ := NewModelTextInputOverlay("claude-sonnet-4-5")
	var _ Overlay = overlay
}

func TestModelTextInputOverlayPrefillsCurrentModel(t *testing.T) {
	overlay, _ := NewModelTextInputOverlay("claude-sonnet-4-5")
	view := overlay.View()
	assert.Contains(t, view, "claude-sonnet-4-5")
}

func TestModelTextInputOverlayNotDoneUntilSubmitted(t *testing.T) {
	overlay, _ := NewModelTextInputOverlay("claude-sonnet-4-5")
	assert.False(t, overlay.Done())
	assert.Nil(t, overlay.Result())
}

func TestModelTextInputOverlaySubmitProducesModelPickerResult(t *testing.T) {
	overlay, initCmd := NewModelTextInputOverlay("claude-sonnet-4-5")
	if initCmd != nil {
		initCmd()
	}

	// Simulate typing over the prefilled field, then submitting. Uses
	// assert.Contains below rather than Equal, since this doesn't assume
	// any specific clear/select-all keybinding for huh.Input (it wraps
	// bubbles/textinput, whose edit shortcuts aren't this test's concern) —
	// only that typed characters end up in the submitted result.
	var updated Overlay
	for _, r := range "claude-opus-4-8" {
		updated, _ = overlay.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		overlay = updated.(*ModelTextInputOverlay)
	}
	updated, _ = overlay.Update(tea.KeyMsg{Type: tea.KeyEnter})
	overlay = updated.(*ModelTextInputOverlay)

	require.True(t, overlay.Done())
	result := overlay.Result()
	require.NotNil(t, result)
	picked, ok := result.(ModelPickerResult)
	require.True(t, ok)
	assert.Contains(t, picked.ModelName, "claude-opus-4-8")
}

func TestModelTextInputOverlayAbortProducesNilResult(t *testing.T) {
	overlay, initCmd := NewModelTextInputOverlay("claude-sonnet-4-5")
	if initCmd != nil {
		initCmd()
	}

	updated, _ := overlay.Update(tea.KeyMsg{Type: tea.KeyEsc})
	overlay = updated.(*ModelTextInputOverlay)

	require.True(t, overlay.Done())
	assert.Nil(t, overlay.Result())
}
