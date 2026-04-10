package interactive

import (
	"fmt"
	"testing"
	"time"
)

func TestACPClientInitWithResumeFlagLoadsSession(t *testing.T) {
	mockStore := &testMockSessionStore{
		sessions: []SessionMetadata{
			{ID: "sess-1", CreatedAt: time.Now(), TurnCount: 3},
		},
		sessionTurns: map[string][]Turn{
			"sess-1": {
				{ID: "turn-1", Timestamp: time.Now(), UserInput: "hello", AgentResp: "hi"},
			},
		},
	}

	mgr := NewSessionManager(mockStore)
	client := NewACPClientWithResume(mgr, "sess-1")

	turns, err := client.LoadedTurns()
	if err != nil {
		t.Fatalf("LoadedTurns failed: %v", err)
	}

	if len(turns) != 1 {
		t.Errorf("expected 1 loaded turn, got %d", len(turns))
	}

	if turns[0].UserInput != "hello" {
		t.Errorf("expected turn input 'hello', got %s", turns[0].UserInput)
	}

	// Verify no load error on successful load
	loadErr := client.LoadError()
	if loadErr != nil {
		t.Errorf("expected LoadError to be nil on successful load, got: %v", loadErr)
	}
}

func TestACPClientNoSessionLoading(t *testing.T) {
	mgr := NewSessionManager(&testMockSessionStore{sessions: []SessionMetadata{}})
	client := NewACPClientWithResume(mgr, "")

	turns, err := client.LoadedTurns()
	if err != nil {
		t.Fatalf("LoadedTurns failed: %v", err)
	}

	if len(turns) != 0 {
		t.Errorf("expected 0 turns when not resuming, got %d", len(turns))
	}
}

func TestACPClientLoadSessionError(t *testing.T) {
	mockStore := &testMockSessionStore{
		sessions:     []SessionMetadata{},
		sessionTurns: map[string][]Turn{},
	}

	mgr := NewSessionManager(mockStore)
	client := NewACPClientWithResume(mgr, "nonexistent-session")

	turns, err := client.LoadedTurns()
	if err != nil {
		t.Fatalf("LoadedTurns failed: %v", err)
	}

	// Should return empty slice when session doesn't exist
	if len(turns) != 0 {
		t.Errorf("expected 0 turns on load error, got %d", len(turns))
	}

	// Verify that the load error is captured
	loadErr := client.LoadError()
	if loadErr == nil {
		t.Errorf("expected LoadError to return an error for nonexistent session, got nil")
	}
	if loadErr.Error() != "load session nonexistent-session: session not found" {
		t.Errorf("expected error containing 'session not found', got: %v", loadErr)
	}
}

// testMockSessionStore implements SessionStore for testing
type testMockSessionStore struct {
	sessions     []SessionMetadata
	sessionTurns map[string][]Turn
}

func (m *testMockSessionStore) ListSessions() ([]SessionMetadata, error) {
	return m.sessions, nil
}

func (m *testMockSessionStore) LoadSession(id string) ([]Turn, error) {
	if turns, ok := m.sessionTurns[id]; ok {
		return turns, nil
	}
	return nil, fmt.Errorf("session not found")
}

func (m *testMockSessionStore) SaveSession(id string, turns []Turn) error {
	return nil
}

func (m *testMockSessionStore) GetSessionMetadata(id string) (SessionMetadata, error) {
	for _, s := range m.sessions {
		if s.ID == id {
			return s, nil
		}
	}
	return SessionMetadata{}, fmt.Errorf("not found")
}
