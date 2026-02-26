package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelPicker_Init(t *testing.T) {
	models := []ModelChoice{
		{Name: "llama3.2:latest", Size: "4.0 GB"},
		{Name: "codellama:7b", Size: "3.5 GB"},
	}
	picker := NewModelPicker(models)
	require.NotNil(t, picker)
	assert.Equal(t, 2, len(picker.models))
	assert.Equal(t, "", picker.Selected())
}

func TestModelPicker_SelectModel(t *testing.T) {
	models := []ModelChoice{
		{Name: "llama3.2:latest", Size: "4.0 GB"},
		{Name: "codellama:7b", Size: "3.5 GB"},
	}
	picker := NewModelPicker(models)

	updated, cmd := picker.Update(tea.KeyMsg{Type: tea.KeyEnter})
	p := updated.(*ModelPicker)
	assert.Equal(t, "llama3.2:latest", p.Selected())
	assert.True(t, p.Done())
	require.NotNil(t, cmd)
}

func TestModelPicker_SingleModel(t *testing.T) {
	models := []ModelChoice{
		{Name: "llama3.2:latest", Size: "4.0 GB"},
	}
	picker := NewModelPicker(models)
	assert.Equal(t, "llama3.2:latest", picker.Selected())
	assert.True(t, picker.Done())
}

func TestModelPicker_Quit(t *testing.T) {
	models := []ModelChoice{
		{Name: "llama3.2:latest", Size: "4.0 GB"},
		{Name: "codellama:7b", Size: "3.5 GB"},
	}
	picker := NewModelPicker(models)

	updated, _ := picker.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	p := updated.(*ModelPicker)
	assert.True(t, p.Cancelled())
}

func TestModelPicker_Navigation(t *testing.T) {
	models := []ModelChoice{
		{Name: "llama3.2:latest", Size: "4.0 GB"},
		{Name: "codellama:7b", Size: "3.5 GB"},
		{Name: "mistral:7b", Size: "4.1 GB"},
	}
	picker := NewModelPicker(models)

	// Press Down to move to second model
	updated, _ := picker.Update(tea.KeyMsg{Type: tea.KeyDown})
	p := updated.(*ModelPicker)
	assert.Equal(t, 1, p.cursor)

	// Press Enter to select second model
	updated, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	p = updated.(*ModelPicker)
	assert.Equal(t, "codellama:7b", p.Selected())
	assert.True(t, p.Done())
	require.NotNil(t, cmd)
}

func TestModelPicker_View(t *testing.T) {
	models := []ModelChoice{
		{Name: "llama3.2:latest", Size: "4.0 GB"},
		{Name: "codellama:7b", Size: "3.5 GB"},
	}
	picker := NewModelPicker(models)

	view := picker.View()
	assert.Contains(t, view, "llama3.2:latest")
	assert.Contains(t, view, "codellama:7b")
	assert.Contains(t, view, "4.0 GB")
	assert.Contains(t, view, "3.5 GB")
	assert.Contains(t, view, "> ")
}

func TestModelPicker_BoundaryUp(t *testing.T) {
	models := []ModelChoice{
		{Name: "llama3.2:latest", Size: "4.0 GB"},
		{Name: "codellama:7b", Size: "3.5 GB"},
	}
	picker := NewModelPicker(models)

	// Cursor starts at 0; pressing Up should stay at 0
	updated, _ := picker.Update(tea.KeyMsg{Type: tea.KeyUp})
	p := updated.(*ModelPicker)
	assert.Equal(t, 0, p.cursor)
}

func TestModelPicker_BoundaryDown(t *testing.T) {
	models := []ModelChoice{
		{Name: "llama3.2:latest", Size: "4.0 GB"},
		{Name: "codellama:7b", Size: "3.5 GB"},
	}
	picker := NewModelPicker(models)

	// Move to last item
	updated, _ := picker.Update(tea.KeyMsg{Type: tea.KeyDown})
	p := updated.(*ModelPicker)
	assert.Equal(t, 1, p.cursor)

	// Pressing Down at last should stay at last
	updated, _ = p.Update(tea.KeyMsg{Type: tea.KeyDown})
	p = updated.(*ModelPicker)
	assert.Equal(t, 1, p.cursor)
}

func TestModelPicker_EscCancels(t *testing.T) {
	models := []ModelChoice{
		{Name: "llama3.2:latest", Size: "4.0 GB"},
		{Name: "codellama:7b", Size: "3.5 GB"},
	}
	picker := NewModelPicker(models)

	updated, cmd := picker.Update(tea.KeyMsg{Type: tea.KeyEsc})
	p := updated.(*ModelPicker)
	assert.True(t, p.Cancelled())
	assert.False(t, p.Done())
	require.NotNil(t, cmd)
}

func TestModelPicker_InitReturnsNil(t *testing.T) {
	models := []ModelChoice{
		{Name: "llama3.2:latest", Size: "4.0 GB"},
	}
	picker := NewModelPicker(models)
	cmd := picker.Init()
	assert.Nil(t, cmd)
}
