package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestInputHistory_AddAndRecall(t *testing.T) {
	h := NewInputHistory(100)
	h.Add("first prompt")
	h.Add("second prompt")

	val, ok := h.Previous("")
	assert.True(t, ok)
	assert.Equal(t, "second prompt", val)

	val, ok = h.Previous(val)
	assert.True(t, ok)
	assert.Equal(t, "first prompt", val)
}

func TestInputHistory_CycleThrough(t *testing.T) {
	h := NewInputHistory(100)
	h.Add("a")
	h.Add("b")
	h.Add("c")

	val, _ := h.Previous("")
	assert.Equal(t, "c", val)
	val, _ = h.Previous(val)
	assert.Equal(t, "b", val)
	val, _ = h.Previous(val)
	assert.Equal(t, "a", val)
	// Past beginning — stays at first
	_, ok := h.Previous(val)
	assert.False(t, ok)
}

func TestInputHistory_RestoreDraft(t *testing.T) {
	h := NewInputHistory(100)
	h.Add("old prompt")

	// Start with draft text, go back, then come forward
	val, _ := h.Previous("my draft")
	assert.Equal(t, "old prompt", val)

	val, ok := h.Next()
	assert.True(t, ok)
	assert.Equal(t, "my draft", val)
}

func TestInputHistory_MaxSize(t *testing.T) {
	h := NewInputHistory(3)
	h.Add("a")
	h.Add("b")
	h.Add("c")
	h.Add("d") // "a" should be evicted
	assert.Equal(t, 3, h.Len())

	val, _ := h.Previous("")
	assert.Equal(t, "d", val)
	val, _ = h.Previous(val)
	assert.Equal(t, "c", val)
	val, _ = h.Previous(val)
	assert.Equal(t, "b", val)
	_, ok := h.Previous(val)
	assert.False(t, ok) // "a" was evicted
}

func TestInputHistory_EmptyHistory(t *testing.T) {
	h := NewInputHistory(100)
	_, ok := h.Previous("")
	assert.False(t, ok)
	_, ok = h.Next()
	assert.False(t, ok)
}

func TestInputHistory_ResetOnAdd(t *testing.T) {
	h := NewInputHistory(100)
	h.Add("a")
	h.Add("b")
	h.Previous("") // cursor at "b"
	h.Add("c")     // adding resets cursor
	val, _ := h.Previous("")
	assert.Equal(t, "c", val)
}

func TestInputHistory_NextPastEnd(t *testing.T) {
	h := NewInputHistory(100)
	h.Add("a")
	// Without calling Previous, Next should be no-op
	_, ok := h.Next()
	assert.False(t, ok)
}

func TestInputHistory_DuplicateNotAdded(t *testing.T) {
	h := NewInputHistory(100)
	h.Add("same")
	h.Add("same")
	assert.Equal(t, 1, h.Len())
}

func TestInputHistory_MidHistoryNext(t *testing.T) {
	h := NewInputHistory(100)
	h.Add("a")
	h.Add("b")
	h.Add("c")

	// Go back to "b"
	val, _ := h.Previous("draft")
	assert.Equal(t, "c", val)
	val, _ = h.Previous(val)
	assert.Equal(t, "b", val)

	// Go forward from mid-history
	val, ok := h.Next()
	assert.True(t, ok)
	assert.Equal(t, "c", val)

	// One more forward restores draft
	val, ok = h.Next()
	assert.True(t, ok)
	assert.Equal(t, "draft", val)
}

func TestInputHistory_MaxSizeClampedToOne(t *testing.T) {
	h := NewInputHistory(0)
	h.Add("only")
	assert.Equal(t, 1, h.Len())
	val, ok := h.Previous("")
	assert.True(t, ok)
	assert.Equal(t, "only", val)
}

func TestModelCtrlPRecallsHistory(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.history.Add("first prompt")
	m.history.Add("second prompt")

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	assert.Equal(t, "second prompt", m.input.Value())

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	assert.Equal(t, "first prompt", m.input.Value())
}

func TestModelCtrlNAdvancesHistory(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	m.history.Add("old")
	m.input.SetValue("draft text")

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	assert.Equal(t, "old", m.input.Value())

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	assert.Equal(t, "draft text", m.input.Value())
}
