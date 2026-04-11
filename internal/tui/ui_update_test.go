package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/julianshen/rubichan/internal/agent"
)

func TestUIUpdateSetsStatusBarProgress(t *testing.T) {
	m := NewModel(nil, "test", "model", 10, "", nil, nil)
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{
		Type: "ui_update",
		UIUpdate: &agent.UIUpdate{
			RequestID: "req-1",
			Status:    "running",
			Message:   "Analyzing codebase...",
		},
	})

	updated, _ := m.Update(evt)
	um := updated.(*Model)

	// Status bar should show progress.
	view := um.statusBar.View()
	assert.Contains(t, view, "Analyzing codebase...")

	// Message should be written to content.
	assert.Contains(t, um.content.String(), "Analyzing codebase...")
}

func TestUIUpdateCompleteClearsProgress(t *testing.T) {
	m := NewModel(nil, "test", "model", 10, "", nil, nil)
	m.state = StateStreaming
	m.statusBar.SetTaskProgress("In progress...")
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{
		Type: "ui_update",
		UIUpdate: &agent.UIUpdate{
			RequestID: "req-1",
			Status:    "complete",
		},
	})

	updated, _ := m.Update(evt)
	um := updated.(*Model)

	// Status bar should be cleared.
	view := um.statusBar.View()
	assert.NotContains(t, view, "In progress...")
}

func TestUIUpdateDoneClearsProgress(t *testing.T) {
	m := NewModel(nil, "test", "model", 10, "", nil, nil)
	m.state = StateStreaming
	m.statusBar.SetTaskProgress("Working...")
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{
		Type: "ui_update",
		UIUpdate: &agent.UIUpdate{
			RequestID: "req-1",
			Status:    "done",
		},
	})

	updated, _ := m.Update(evt)
	um := updated.(*Model)

	view := um.statusBar.View()
	assert.NotContains(t, view, "Working...")
}

func TestUIUpdateStatusFallbackWhenNoMessage(t *testing.T) {
	m := NewModel(nil, "test", "model", 10, "", nil, nil)
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{
		Type: "ui_update",
		UIUpdate: &agent.UIUpdate{
			RequestID: "req-1",
			Status:    "scanning",
		},
	})

	updated, _ := m.Update(evt)
	um := updated.(*Model)

	// Should fall back to showing status when message is empty.
	view := um.statusBar.View()
	assert.Contains(t, view, "scanning")

	// No message written to content buffer when Message is empty.
	assert.NotContains(t, um.content.String(), "scanning")
}

func TestUIUpdateNilUpdateIgnored(t *testing.T) {
	m := NewModel(nil, "test", "model", 10, "", nil, nil)
	m.state = StateStreaming
	ch := make(chan agent.TurnEvent, 1)
	ch <- agent.TurnEvent{Type: "done"}
	m.eventCh = ch

	evt := TurnEventMsg(agent.TurnEvent{
		Type:     "ui_update",
		UIUpdate: nil,
	})

	updated, _ := m.Update(evt)
	um := updated.(*Model)

	// Nothing should crash or change.
	assert.Equal(t, StateStreaming, um.state)
}
