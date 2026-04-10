package interactive

import (
	"testing"
	"time"
)

func TestInteractiveStartupWithSessionManager(t *testing.T) {
	// Create mock store with a test session
	mockSessions := []SessionMetadata{
		{ID: "sess-1", CreatedAt: time.Now(), TurnCount: 3},
	}
	mockStore := &mockSessionStore{sessions: mockSessions}

	// Create session manager with mock store
	sessionMgr := NewSessionManager(mockStore)

	// Create interactive TUI with session manager
	tui := NewInteractiveTUI(sessionMgr, nil)

	// Should have session manager available
	if tui.sessionMgr == nil {
		t.Error("expected sessionMgr to be set")
	}

	// Should be able to list sessions through TUI
	sessions, err := tui.sessionMgr.List()
	if err != nil {
		t.Fatalf("failed to list sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(sessions))
	}
}
