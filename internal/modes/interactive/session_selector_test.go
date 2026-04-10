package interactive

import (
	"testing"
	"time"
)

func TestSessionSelectorInit(t *testing.T) {
	sessions := []SessionMetadata{
		{ID: "sess-1", CreatedAt: time.Now(), TurnCount: 5},
		{ID: "sess-2", CreatedAt: time.Now().Add(-1 * time.Hour), TurnCount: 3},
	}

	sel := NewSessionSelector(sessions)

	if sel.SelectedIndex() != 0 {
		t.Errorf("expected selected index 0, got %d", sel.SelectedIndex())
	}

	if len(sel.Sessions()) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sel.Sessions()))
	}
}

func TestSessionSelectorSelectSession(t *testing.T) {
	sessions := []SessionMetadata{
		{ID: "sess-1", CreatedAt: time.Now(), TurnCount: 5},
		{ID: "sess-2", CreatedAt: time.Now().Add(-1 * time.Hour), TurnCount: 3},
	}

	sel := NewSessionSelector(sessions)
	sel.MoveDown()

	if sel.SelectedIndex() != 1 {
		t.Errorf("expected selected index 1 after MoveDown, got %d", sel.SelectedIndex())
	}

	selected := sel.Selected()
	if selected.ID != "sess-2" {
		t.Errorf("expected selected session ID sess-2, got %s", selected.ID)
	}
}

func TestSessionSelectorMoveUp(t *testing.T) {
	sessions := []SessionMetadata{
		{ID: "sess-1", CreatedAt: time.Now(), TurnCount: 5},
		{ID: "sess-2", CreatedAt: time.Now().Add(-1 * time.Hour), TurnCount: 3},
	}

	sel := NewSessionSelector(sessions)
	sel.MoveDown()
	sel.MoveUp()

	if sel.SelectedIndex() != 0 {
		t.Errorf("expected index 0 after MoveUp, got %d", sel.SelectedIndex())
	}
}

func TestSessionSelectorBoundaries(t *testing.T) {
	sessions := []SessionMetadata{
		{ID: "sess-1", CreatedAt: time.Now(), TurnCount: 5},
		{ID: "sess-2", CreatedAt: time.Now().Add(-1 * time.Hour), TurnCount: 3},
	}

	sel := NewSessionSelector(sessions)

	// Can't move up from 0
	sel.MoveUp()
	if sel.SelectedIndex() != 0 {
		t.Errorf("expected index 0 after MoveUp at boundary, got %d", sel.SelectedIndex())
	}

	// Move to end
	sel.MoveDown()
	sel.MoveDown() // Try to go past end
	if sel.SelectedIndex() != 1 {
		t.Errorf("expected index 1 at boundary, got %d", sel.SelectedIndex())
	}
}

func TestSessionSelectorReset(t *testing.T) {
	sessions := []SessionMetadata{
		{ID: "sess-1", CreatedAt: time.Now(), TurnCount: 5},
		{ID: "sess-2", CreatedAt: time.Now().Add(-1 * time.Hour), TurnCount: 3},
	}

	sel := NewSessionSelector(sessions)
	sel.MoveDown()
	sel.Reset()

	if sel.SelectedIndex() != 0 {
		t.Errorf("expected index 0 after Reset, got %d", sel.SelectedIndex())
	}
}

func TestSessionSelectorSelectedReturnsEmpty(t *testing.T) {
	sel := NewSessionSelector([]SessionMetadata{})
	selected := sel.Selected()

	if selected.ID != "" {
		t.Errorf("expected empty SessionMetadata for empty selector, got %v", selected)
	}
}
