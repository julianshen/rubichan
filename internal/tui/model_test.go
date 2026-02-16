package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewModel(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")

	assert.Equal(t, StateInput, m.state)
	assert.Equal(t, "rubichan", m.appName)
	assert.Equal(t, "claude-3", m.modelName)
	assert.Equal(t, 80, m.width)
	assert.Equal(t, 24, m.height)
	assert.False(t, m.quitting)
	assert.NotNil(t, m.input)
	assert.NotNil(t, m.viewport)
	assert.NotNil(t, m.spinner)
}

func TestModelHandleSlashQuit(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	cmd := m.handleCommand("/quit")

	require.NotNil(t, cmd, "handleCommand(/quit) should return a non-nil tea.Cmd")
	assert.True(t, m.quitting)

	// Verify it produces a tea.Quit message
	msg := cmd()
	assert.IsType(t, tea.QuitMsg{}, msg)
}

func TestModelHandleSlashExit(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	cmd := m.handleCommand("/exit")

	require.NotNil(t, cmd, "handleCommand(/exit) should return a non-nil tea.Cmd")
	assert.True(t, m.quitting)
}

func TestModelHandleSlashClear(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")

	// Write some content first
	m.content.WriteString("some previous content")

	cmd := m.handleCommand("/clear")

	assert.Nil(t, cmd, "handleCommand(/clear) should return nil (doesn't quit)")
	assert.Equal(t, "", m.content.String())
}

func TestModelHandleSlashHelp(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	cmd := m.handleCommand("/help")

	assert.Nil(t, cmd, "handleCommand(/help) should return nil")
	content := m.content.String()
	assert.Contains(t, content, "/quit")
	assert.Contains(t, content, "/clear")
	assert.Contains(t, content, "/model")
	assert.Contains(t, content, "/help")
}

func TestModelHandleSlashModel(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	cmd := m.handleCommand("/model gpt-4")

	assert.Nil(t, cmd, "handleCommand(/model) should return nil")
	assert.Equal(t, "gpt-4", m.modelName)
	assert.True(t, strings.Contains(m.content.String(), "Model switched"))
}

func TestModelHandleSlashModelNoArg(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	cmd := m.handleCommand("/model")

	assert.Nil(t, cmd)
	assert.Equal(t, "claude-3", m.modelName, "model should not change without argument")
	assert.Contains(t, m.content.String(), "Usage:")
}

func TestModelHandleUnknownCommand(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3")
	cmd := m.handleCommand("/unknown")

	assert.Nil(t, cmd)
	assert.Contains(t, m.content.String(), "Unknown command")
}
