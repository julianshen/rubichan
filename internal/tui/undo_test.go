package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/julianshen/rubichan/internal/checkpoint"
	"github.com/stretchr/testify/assert"
)

func makeCheckpoints() []checkpoint.Checkpoint {
	return []checkpoint.Checkpoint{
		{ID: "a", FilePath: "/src/main.go", Turn: 2, Operation: "write", Timestamp: time.Now()},
		{ID: "b", FilePath: "/pkg/api.go", Turn: 3, Operation: "patch", Timestamp: time.Now()},
		{ID: "c", FilePath: "/src/main.go", Turn: 3, Operation: "write", Timestamp: time.Now()},
	}
}

func TestUndoOverlayImplementsOverlay(t *testing.T) {
	o := NewUndoOverlay(makeCheckpoints(), 80)
	var _ Overlay = o
	assert.False(t, o.Done())
	assert.Contains(t, o.View(), "main.go")
}

func TestUndoOverlayNavigateDown(t *testing.T) {
	o := NewUndoOverlay(makeCheckpoints(), 80)
	assert.Equal(t, 0, o.selected)
	o.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 1, o.selected)
}

func TestUndoOverlayNavigateWraps(t *testing.T) {
	o := NewUndoOverlay(makeCheckpoints(), 80)
	o.Update(tea.KeyMsg{Type: tea.KeyUp}) // wraps to last
	assert.Equal(t, 2, o.selected)
}

func TestUndoOverlayEnterConfirms(t *testing.T) {
	o := NewUndoOverlay(makeCheckpoints(), 80)
	updated, _ := o.Update(tea.KeyMsg{Type: tea.KeyEnter})
	u := updated.(*UndoOverlay)
	assert.True(t, u.Done())
	result := u.Result().(UndoResult)
	// Displayed newest-first: c(turn 3), b(turn 3), a(turn 2). selected=0 → turn 3
	assert.Equal(t, 3, result.Turn)
	assert.False(t, result.All)
}

func TestUndoOverlayEscapeCancels(t *testing.T) {
	o := NewUndoOverlay(makeCheckpoints(), 80)
	updated, _ := o.Update(tea.KeyMsg{Type: tea.KeyEscape})
	u := updated.(*UndoOverlay)
	assert.True(t, u.Done())
	assert.Nil(t, u.Result())
}

func TestUndoOverlayRewindAll(t *testing.T) {
	o := NewUndoOverlay(makeCheckpoints(), 80)
	updated, _ := o.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	u := updated.(*UndoOverlay)
	assert.True(t, u.Done())
	result := u.Result().(UndoResult)
	assert.True(t, result.All)
	assert.Equal(t, 3, result.Turn)
}

func TestUndoOverlayEmptyCheckpoints(t *testing.T) {
	o := NewUndoOverlay(nil, 80)
	assert.Contains(t, o.View(), "No checkpoints")
	updated, _ := o.Update(tea.KeyMsg{Type: tea.KeyEnter})
	u := updated.(*UndoOverlay)
	assert.True(t, u.Done())
	assert.Nil(t, u.Result())
}
