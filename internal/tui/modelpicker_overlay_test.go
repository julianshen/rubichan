package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/commands"
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

func TestModelCommandNoArgsOpensPickerOverlay(t *testing.T) {
	reg := commands.NewRegistry()
	m := NewModel(nil, "test", "model", 10, "", nil, reg)

	require.NoError(t, reg.Register(commands.NewModelCommand(func(string) {})))

	cmd := m.handleCommand("/model")
	assert.NotNil(t, cmd) // huh form Init returns a command
	assert.Equal(t, StateModelPickerOverlay, m.state)
	assert.NotNil(t, m.activeOverlay)
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
