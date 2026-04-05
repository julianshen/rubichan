package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/julianshen/rubichan/internal/config"
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

func TestApprovalOverlayImplementsOverlay(t *testing.T) {
	o := NewApprovalOverlay("shell", `command="ls"`, "/tmp", 80,
		[]ApprovalResult{ApprovalYes, ApprovalNo, ApprovalAlways}, false)
	var _ Overlay = o // compile-time check
	assert.False(t, o.Done())
	assert.Contains(t, o.View(), "Bash") // "shell" tool displays as "Bash"
}

func TestApprovalOverlayHandlesYes(t *testing.T) {
	o := NewApprovalOverlay("shell", `command="ls"`, "/tmp", 80,
		[]ApprovalResult{ApprovalYes, ApprovalNo}, false)
	updated, _ := o.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	overlay := updated.(*ApprovalOverlay)
	assert.True(t, overlay.Done())
	assert.Equal(t, ApprovalYes, overlay.Result())
}

func TestApprovalOverlayRejectsDisallowedKey(t *testing.T) {
	o := NewApprovalOverlay("shell", `command="rm -rf /"`, "/tmp", 80,
		[]ApprovalResult{ApprovalYes, ApprovalNo}, false) // no Always for destructive
	updated, _ := o.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	overlay := updated.(*ApprovalOverlay)
	assert.False(t, overlay.Done()) // 'a' not allowed
}

func TestConfigOverlayImplementsOverlay(t *testing.T) {
	cfg := &config.Config{}
	o, initCmd := NewConfigOverlay(cfg, "")
	var _ Overlay = o
	assert.False(t, o.Done())
	assert.NotNil(t, initCmd)
}

func TestWikiOverlayImplementsOverlay(t *testing.T) {
	o, initCmd := NewWikiOverlay("/tmp")
	var _ Overlay = o
	assert.False(t, o.Done())
	assert.NotNil(t, initCmd)
}
