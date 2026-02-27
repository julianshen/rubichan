package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHuhModelPickerCreation(t *testing.T) {
	models := []ModelChoice{
		{Name: "llama3", Size: "8B"},
		{Name: "mistral", Size: "7B"},
	}
	p := NewModelPicker(models)
	require.NotNil(t, p)
	assert.False(t, p.Done())
	assert.False(t, p.Cancelled())
	// huh.Select initializes value to the first option, but Done() is false
	// indicating no selection has been confirmed yet.
}

func TestHuhModelPickerAutoSelect(t *testing.T) {
	models := []ModelChoice{
		{Name: "llama3", Size: "8B"},
	}
	p := NewModelPicker(models)
	assert.True(t, p.Done())
	assert.Equal(t, "llama3", p.Selected())
}

func TestHuhModelPickerEmptyModels(t *testing.T) {
	p := NewModelPicker([]ModelChoice{})
	assert.True(t, p.Done())
	assert.Equal(t, "", p.Selected())
}

func TestHuhModelPickerView(t *testing.T) {
	models := []ModelChoice{
		{Name: "llama3", Size: "8B"},
		{Name: "mistral", Size: "7B"},
	}
	p := NewModelPicker(models)
	p.Init()
	view := p.View()
	assert.Contains(t, view, "llama3")
	assert.Contains(t, view, "mistral")
}

func TestHuhModelPickerInitWithForm(t *testing.T) {
	models := []ModelChoice{
		{Name: "llama3", Size: "8B"},
		{Name: "mistral", Size: "7B"},
	}
	p := NewModelPicker(models)
	cmd := p.Init()
	// huh.Form.Init() returns a command batch
	assert.NotNil(t, cmd)
}

func TestHuhModelPickerInitWithoutForm(t *testing.T) {
	// Single model auto-selects, no form created
	models := []ModelChoice{
		{Name: "llama3", Size: "8B"},
	}
	p := NewModelPicker(models)
	cmd := p.Init()
	assert.Nil(t, cmd)
}

func TestHuhModelPickerUpdateWithoutForm(t *testing.T) {
	// Single model auto-selects, form is nil
	models := []ModelChoice{
		{Name: "llama3", Size: "8B"},
	}
	p := NewModelPicker(models)
	updated, cmd := p.Update(nil)
	assert.Equal(t, p, updated)
	assert.Nil(t, cmd)
}

func TestHuhModelPickerViewWithoutForm(t *testing.T) {
	// Single model auto-selects, form is nil
	models := []ModelChoice{
		{Name: "llama3", Size: "8B"},
	}
	p := NewModelPicker(models)
	view := p.View()
	assert.Equal(t, "", view)
}

func TestHuhModelPickerTeaModelInterface(t *testing.T) {
	// Compile-time check is in the production code,
	// but we verify the methods exist via the public API.
	models := []ModelChoice{
		{Name: "llama3", Size: "8B"},
		{Name: "mistral", Size: "7B"},
	}
	p := NewModelPicker(models)

	// All three tea.Model methods should be callable
	_ = p.Init()
	_, _ = p.Update(nil)
	_ = p.View()
}
