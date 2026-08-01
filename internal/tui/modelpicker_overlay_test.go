package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/internal/config"
)

func TestModelPickerOverlayImplementsOverlay(t *testing.T) {
	overlay, _ := NewModelPickerOverlay([]ModelChoice{
		{Name: "gpt-4o", Size: "large"},
	})
	var _ Overlay = overlay
}

func TestModelPickerOverlayAutoSelectSingleModel(t *testing.T) {
	overlay, _ := NewModelPickerOverlay([]ModelChoice{
		{Name: "gpt-4o", Size: "large"},
	})

	assert.True(t, overlay.Done())
	result := overlay.Result()
	require.NotNil(t, result)

	picked, ok := result.(ModelPickerResult)
	require.True(t, ok)
	assert.Equal(t, "gpt-4o", picked.ModelName)
}

func TestModelPickerOverlayEmptyModels(t *testing.T) {
	overlay, _ := NewModelPickerOverlay(nil)

	assert.True(t, overlay.Done())
	assert.Nil(t, overlay.Result())
}

func TestProcessOverlayResultModelPicker(t *testing.T) {
	m := NewModel(nil, "test", "model", 10, "", nil, nil)
	m.state = StateModelPickerOverlay

	// Record content length before to check only new content.
	before := m.content.String()
	cmd := m.processOverlayResult(ModelPickerResult{ModelName: "claude-sonnet-4-6"})

	assert.Nil(t, cmd)
	assert.Equal(t, StateInput, m.state)

	// New content should contain the switched model.
	newContent := m.content.String()[len(before):]
	assert.Contains(t, newContent, "claude-sonnet-4-6")
	assert.Equal(t, "claude-sonnet-4-6", m.modelName)
}

func TestProcessOverlayResultModelPickerCancelled(t *testing.T) {
	m := NewModel(nil, "test", "model", 10, "", nil, nil)
	m.state = StateModelPickerOverlay

	cmd := m.processOverlayResult(nil)

	assert.Nil(t, cmd)
	assert.Equal(t, StateInput, m.state)
}

func TestModelCommandNoArgsWithNilConfigShowsMessage(t *testing.T) {
	reg := commands.NewRegistry()
	m := NewModel(nil, "test", "model", 10, "", nil, reg)

	require.NoError(t, reg.Register(commands.NewModelCommand(func(string) {})))

	before := m.content.String()
	cmd := m.handleCommand("/model")
	assert.Nil(t, cmd)
	assert.Equal(t, StateInput, m.state)
	assert.Nil(t, m.activeOverlay)
	assert.Contains(t, m.content.String()[len(before):], "No config available")
}

func TestModelCommandNoArgsOpensTextInputForNonOllama(t *testing.T) {
	reg := commands.NewRegistry()
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "anthropic"
	m := NewModel(nil, "test", "model", 10, "", cfg, reg)

	require.NoError(t, reg.Register(commands.NewModelCommand(func(string) {})))

	cmd := m.handleCommand("/model")
	assert.NotNil(t, cmd) // huh form Init returns a command
	assert.Equal(t, StateModelPickerOverlay, m.state)
	require.NotNil(t, m.activeOverlay)
	_, ok := m.activeOverlay.(*ModelTextInputOverlay)
	assert.True(t, ok, "non-ollama providers get the text-input overlay, not the selection-list picker")
}

func TestModelCommandNoArgsFetchesForOllama(t *testing.T) {
	reg := commands.NewRegistry()
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "ollama"
	m := NewModel(nil, "test", "model", 10, "", cfg, reg)

	require.NoError(t, reg.Register(commands.NewModelCommand(func(string) {})))

	cmd := m.handleCommand("/model")
	assert.NotNil(t, cmd) // fetchOllamaModels + spinner.Tick, batched
	assert.Equal(t, StateFetchingModels, m.state)
	assert.Nil(t, m.activeOverlay, "overlay doesn't open until ModelsFetchedMsg arrives")
}

func TestModelCommandWithArgSwitchesDirectly(t *testing.T) {
	reg := commands.NewRegistry()
	var switched string
	m := NewModel(nil, "test", "model", 10, "", nil, reg)

	require.NoError(t, reg.Register(commands.NewModelCommand(func(name string) { switched = name })))

	cmd := m.handleCommand("/model gpt-4o")
	assert.Nil(t, cmd)
	assert.Equal(t, StateInput, m.state)
	assert.Equal(t, "gpt-4o", switched)
}
