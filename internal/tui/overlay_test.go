package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

type mockOverlay struct {
	done   bool
	result any
}

func (m *mockOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) { return m, nil }
func (m *mockOverlay) View() string                          { return "[mock overlay]" }
func (m *mockOverlay) Done() bool                            { return m.done }
func (m *mockOverlay) Result() any                           { return m.result }

func TestOverlayInterfaceContract(t *testing.T) {
	var o Overlay = &mockOverlay{done: false, result: nil}
	assert.False(t, o.Done())
	assert.Nil(t, o.Result())
	assert.Equal(t, "[mock overlay]", o.View())

	updated, cmd := o.Update(tea.KeyMsg{})
	assert.NotNil(t, updated)
	assert.Nil(t, cmd)
}

func TestOverlayDoneReturnsResult(t *testing.T) {
	o := &mockOverlay{done: true, result: "test-result"}
	assert.True(t, o.Done())
	assert.Equal(t, "test-result", o.Result())
}

func TestOverlayNilResultOnCancel(t *testing.T) {
	o := &mockOverlay{done: true, result: nil}
	assert.True(t, o.Done())
	assert.Nil(t, o.Result())
}
