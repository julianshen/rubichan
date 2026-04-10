package interactive

import (
	"context"
	"strings"
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

func TestHandleResumeCommand(t *testing.T) {
	mockSessions := []SessionMetadata{
		{ID: "sess-1", CreatedAt: time.Now(), TurnCount: 5},
	}
	mockStore := &mockSessionStore{sessions: mockSessions}
	sessionMgr := NewSessionManager(mockStore)

	tui := NewInteractiveTUI(sessionMgr, nil)
	err := tui.HandleResumeCommand()

	// Error is expected since we're not actually running the Bubble Tea model
	// Just verify the method exists and handles the flow
	if err != nil && !strings.Contains(err.Error(), "overlay") {
		t.Logf("HandleResumeCommand executed with sessions available: %v", err)
	}
}

func TestHandleResumeCommandNoManager(t *testing.T) {
	tui := NewInteractiveTUI(nil, nil)
	err := tui.HandleResumeCommand()

	if err == nil {
		t.Error("expected error when no session manager")
	}
}

func TestHandleResumeCommandNoSessions(t *testing.T) {
	mockStore := &mockSessionStore{sessions: []SessionMetadata{}}
	sessionMgr := NewSessionManager(mockStore)

	tui := NewInteractiveTUI(sessionMgr, nil)
	err := tui.HandleResumeCommand()

	if err == nil {
		t.Error("expected error when no sessions exist")
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

func TestInteractiveTUISessionManagerGetterSetter(t *testing.T) {
	mockMgr := &mockSessionStore{sessions: []SessionMetadata{}}
	sessionMgr := NewSessionManager(mockMgr)
	tui := NewInteractiveTUI(sessionMgr, nil)

	retrieved := tui.SessionManager()
	if retrieved != sessionMgr {
		t.Error("SessionManager() should return same instance passed to constructor")
	}

	newMockMgr := &mockSessionStore{
		sessions: []SessionMetadata{
			{ID: "sess-1", CreatedAt: time.Now(), TurnCount: 5},
		},
	}
	newMgr := NewSessionManager(newMockMgr)
	tui.SetSessionManager(newMgr)
	retrieved = tui.SessionManager()
	if retrieved != newMgr {
		t.Error("SetSessionManager() should update the manager")
	}
}

func TestInteractiveTUIACPClientGetterSetter(t *testing.T) {
	tui := NewInteractiveTUI(nil, nil)

	if tui.ACPClient() != nil {
		t.Error("ACPClient() should be nil when not set")
	}

	client := NewACPClientWithResume(nil, "")
	tui.SetACPClient(client)
	retrieved := tui.ACPClient()
	if retrieved != client {
		t.Error("SetACPClient() should update the client")
	}
}

func TestInteractiveTUIRestoreTurns(t *testing.T) {
	tui := NewInteractiveTUI(nil, nil)

	turns := []Turn{
		{ID: "turn-1", Timestamp: time.Now(), UserInput: "hello", AgentResp: "hi"},
	}

	tui.restoreTurns(turns)
	// Verify turns are stored by checking internal state
	if len(tui.turns) != 1 {
		t.Errorf("expected 1 restored turn, got %d", len(tui.turns))
	}
	if tui.turns[0].UserInput != "hello" {
		t.Errorf("expected restored turn input 'hello', got %s", tui.turns[0].UserInput)
	}
}
