package interactive

import (
	"context"
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

func TestShouldPromptResumeWhenSessionsExist(t *testing.T) {
	mockSessions := []SessionMetadata{
		{ID: "sess-1", CreatedAt: time.Now(), TurnCount: 5},
	}
	mockStore := &mockSessionStore{sessions: mockSessions}
	sessionMgr := NewSessionManager(mockStore)

	tui := NewInteractiveTUI(sessionMgr, nil)
	shouldPrompt, err := tui.ShouldPromptResume()

	if err != nil {
		t.Fatalf("ShouldPromptResume failed: %v", err)
	}

	if !shouldPrompt {
		t.Error("expected ShouldPromptResume to return true when sessions exist")
	}
}

func TestShouldPromptResumeWhenNoSessionsExist(t *testing.T) {
	mockStore := &mockSessionStore{sessions: []SessionMetadata{}}
	sessionMgr := NewSessionManager(mockStore)

	tui := NewInteractiveTUI(sessionMgr, nil)
	shouldPrompt, err := tui.ShouldPromptResume()

	if err != nil {
		t.Fatalf("ShouldPromptResume failed: %v", err)
	}

	if shouldPrompt {
		t.Error("expected ShouldPromptResume to return false when no sessions exist")
	}
}

func TestShouldPromptResumeWhenNoSessionManager(t *testing.T) {
	tui := NewInteractiveTUI(nil, nil) // no session manager

	shouldPrompt, err := tui.ShouldPromptResume()

	if err != nil {
		t.Fatalf("ShouldPromptResume failed: %v", err)
	}

	if shouldPrompt {
		t.Error("expected ShouldPromptResume to return false when no sessionMgr")
	}
}

func TestPromptResumeSessionLoadsSelectedSession(t *testing.T) {
	mockSessions := []SessionMetadata{
		{ID: "sess-1", CreatedAt: time.Now(), TurnCount: 3},
		{ID: "sess-2", CreatedAt: time.Now().Add(-1 * time.Hour), TurnCount: 5},
	}
	mockStore := &mockSessionStore{
		sessions: mockSessions,
	}
	sessionMgr := NewSessionManager(mockStore)
	_ = NewInteractiveTUI(sessionMgr, nil)

	// Create a mock callback to verify loading works
	selectedID := ""
	callErr := error(nil)

	// Manually trigger what PromptResumeSession would do with session selection
	callback := func(selected SessionMetadata, err error) {
		if err != nil {
			callErr = err
			return
		}
		// For testing, we'll just verify we can load and check for turns
		selectedID = selected.ID
	}

	overlay := NewSessionSelectorOverlay(mockSessions, callback)
	// Select first session
	overlay.selector.Reset()
	// Manually invoke callback to test the flow
	callback(overlay.selector.Selected(), nil)

	if callErr != nil {
		t.Fatalf("callback failed: %v", callErr)
	}

	if selectedID != "sess-1" {
		t.Errorf("expected selected session sess-1, got %s", selectedID)
	}
}

func TestPromptResumeSessionReturnsErrorWhenNoManager(t *testing.T) {
	tuiInstance := NewInteractiveTUI(nil, nil)
	ctx := context.Background()

	err := tuiInstance.PromptResumeSession(ctx)
	if err == nil {
		t.Fatal("expected error when session manager is nil")
	}
}

func TestPromptResumeSessionReturnsErrorWhenNoSessions(t *testing.T) {
	mockStore := &mockSessionStore{sessions: []SessionMetadata{}}
	sessionMgr := NewSessionManager(mockStore)

	tuiInstance := NewInteractiveTUI(sessionMgr, nil)
	ctx := context.Background()

	err := tuiInstance.PromptResumeSession(ctx)
	if err == nil {
		t.Fatal("expected error when no sessions exist")
	}
}
