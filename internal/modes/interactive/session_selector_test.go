package interactive

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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

func TestSessionSelectorDefensiveCopyInConstructor(t *testing.T) {
	// Verify that caller cannot mutate selector via the original slice
	sessions := []SessionMetadata{
		{ID: "sess-1", CreatedAt: time.Now(), TurnCount: 5},
		{ID: "sess-2", CreatedAt: time.Now().Add(-1 * time.Hour), TurnCount: 3},
	}

	sel := NewSessionSelector(sessions)

	// Mutate original slice
	sessions[0].ID = "mutated"

	// Selector should not be affected
	if sel.Sessions()[0].ID != "sess-1" {
		t.Errorf("selector was mutated by caller: expected sess-1, got %s", sel.Sessions()[0].ID)
	}
}

func TestSessionSelectorSessionsReturnsCopy(t *testing.T) {
	// Verify that caller cannot mutate selector via Sessions() return value
	sessions := []SessionMetadata{
		{ID: "sess-1", CreatedAt: time.Now(), TurnCount: 5},
		{ID: "sess-2", CreatedAt: time.Now().Add(-1 * time.Hour), TurnCount: 3},
	}

	sel := NewSessionSelector(sessions)
	retrieved := sel.Sessions()

	// Mutate returned slice
	retrieved[0].ID = "mutated"

	// Selector should not be affected
	if sel.Sessions()[0].ID != "sess-1" {
		t.Errorf("selector was mutated via Sessions() return: expected sess-1, got %s", sel.Sessions()[0].ID)
	}
}

func TestSessionSelectorOverlayRender(t *testing.T) {
	sessions := []SessionMetadata{
		{ID: "sess-1", CreatedAt: time.Now(), TurnCount: 5},
	}

	overlay := NewSessionSelectorOverlay(sessions, nil)
	output := overlay.View()

	if output == "" {
		t.Error("expected non-empty View output")
	}

	// Should contain session ID
	if !contains(output, "sess-1") {
		t.Error("expected output to contain session ID sess-1")
	}

	// Should contain turn count
	if !contains(output, "5") {
		t.Error("expected output to contain turn count")
	}
}

func TestSessionSelectorOverlayKeyNavigation(t *testing.T) {
	sessions := []SessionMetadata{
		{ID: "sess-1", CreatedAt: time.Now(), TurnCount: 5},
		{ID: "sess-2", CreatedAt: time.Now().Add(-1 * time.Hour), TurnCount: 3},
	}

	overlay := NewSessionSelectorOverlay(sessions, func(s SessionMetadata, err error) {
		// callback not used in this test
	})

	// Simulate down arrow
	_, _ = overlay.Update(tea.KeyMsg{Type: tea.KeyDown})
	if overlay.selector.SelectedIndex() != 1 {
		t.Errorf("expected index 1 after down, got %d", overlay.selector.SelectedIndex())
	}

	// Simulate up arrow
	_, _ = overlay.Update(tea.KeyMsg{Type: tea.KeyUp})
	if overlay.selector.SelectedIndex() != 0 {
		t.Errorf("expected index 0 after up, got %d", overlay.selector.SelectedIndex())
	}
}

func TestSessionSelectorOverlayEnter(t *testing.T) {
	sessions := []SessionMetadata{
		{ID: "sess-1", CreatedAt: time.Now(), TurnCount: 5},
	}

	selectedSession := SessionMetadata{}
	overlay := NewSessionSelectorOverlay(sessions, func(s SessionMetadata, err error) {
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		selectedSession = s
	})

	_, _ = overlay.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if selectedSession.ID != "sess-1" {
		t.Errorf("expected selected session ID sess-1, got %s", selectedSession.ID)
	}
}

func TestSessionSelectorOverlayCancel(t *testing.T) {
	sessions := []SessionMetadata{
		{ID: "sess-1", CreatedAt: time.Now(), TurnCount: 5},
	}

	var errReceived error
	overlay := NewSessionSelectorOverlay(sessions, func(s SessionMetadata, err error) {
		errReceived = err
	})

	_, _ = overlay.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if errReceived == nil {
		t.Error("expected error on cancel, got nil")
	}
}

func TestSessionSelectorOverlayVimNavigation(t *testing.T) {
	sessions := []SessionMetadata{
		{ID: "sess-1", CreatedAt: time.Now(), TurnCount: 5},
		{ID: "sess-2", CreatedAt: time.Now().Add(-1 * time.Hour), TurnCount: 3},
	}

	overlay := NewSessionSelectorOverlay(sessions, nil)

	// Test 'j' moves down
	_, _ = overlay.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if overlay.selector.SelectedIndex() != 1 {
		t.Errorf("expected 'j' to move down to index 1, got %d", overlay.selector.SelectedIndex())
	}

	// Test 'k' moves up
	_, _ = overlay.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if overlay.selector.SelectedIndex() != 0 {
		t.Errorf("expected 'k' to move up to index 0, got %d", overlay.selector.SelectedIndex())
	}
}

func TestSessionSelectorOverlayQuitKey(t *testing.T) {
	sessions := []SessionMetadata{
		{ID: "sess-1", CreatedAt: time.Now(), TurnCount: 5},
	}

	errReceived := false
	overlay := NewSessionSelectorOverlay(sessions, func(s SessionMetadata, err error) {
		if err != nil {
			errReceived = true
		}
	})

	_, _ = overlay.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	if !errReceived {
		t.Error("expected 'q' to cancel with error")
	}
}

func TestTimeAgoEdgeCases(t *testing.T) {
	now := time.Now()

	// 30 seconds ago
	result := timeAgo(now.Add(-30 * time.Second))
	if result != "just now" {
		t.Errorf("expected 'just now' for 30 seconds, got %s", result)
	}

	// 59 seconds ago
	result = timeAgo(now.Add(-59 * time.Second))
	if result != "just now" {
		t.Errorf("expected 'just now' for 59 seconds, got %s", result)
	}

	// 1 minute ago
	result = timeAgo(now.Add(-1 * time.Minute))
	if result != "1 min ago" {
		t.Errorf("expected '1 min ago' for 1 minute, got %s", result)
	}

	// 5 minutes ago
	result = timeAgo(now.Add(-5 * time.Minute))
	if result != "5 min ago" {
		t.Errorf("expected '5 min ago' for 5 minutes, got %s", result)
	}

	// 1 hour ago
	result = timeAgo(now.Add(-1 * time.Hour))
	if result != "1 hours ago" {
		t.Errorf("expected '1 hours ago' for 1 hour, got %s", result)
	}

	// 2 hours ago
	result = timeAgo(now.Add(-2 * time.Hour))
	if result != "2 hours ago" {
		t.Errorf("expected '2 hours ago' for 2 hours, got %s", result)
	}

	// 23 hours ago (still in hours range)
	result = timeAgo(now.Add(-23 * time.Hour))
	if result != "23 hours ago" {
		t.Errorf("expected '23 hours ago' for 23 hours, got %s", result)
	}

	// 24 hours ago (switches to date format)
	result = timeAgo(now.Add(-24 * time.Hour))
	if !strings.Contains(result, "ago") && !strings.Contains(result, ",") {
		t.Errorf("expected date format for 24 hours, got %s", result)
	}

	// 7 days ago (should be date format)
	result = timeAgo(now.Add(-7 * 24 * time.Hour))
	if !strings.Contains(result, ",") {
		t.Errorf("expected date format for 7 days, got %s", result)
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
