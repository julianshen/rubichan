package interactive

import (
	"testing"
	"time"
)

func TestSessionManagerListSessions(t *testing.T) {
	// Mock persistence layer that returns 3 sessions
	mockStore := &mockSessionStore{
		sessions: []SessionMetadata{
			{ID: "sess-001", CreatedAt: time.Now().Add(-24 * time.Hour), TurnCount: 5},
			{ID: "sess-002", CreatedAt: time.Now().Add(-2 * time.Hour), TurnCount: 12},
			{ID: "sess-003", CreatedAt: time.Now(), TurnCount: 0},
		},
	}

	sm := NewSessionManager(mockStore)
	sessions, err := sm.List()

	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}
	if len(sessions) != 3 {
		t.Errorf("expected 3 sessions, got %d", len(sessions))
	}
	// Most recent first
	if sessions[0].ID != "sess-003" {
		t.Errorf("expected first session ID sess-003, got %s", sessions[0].ID)
	}
}

type mockSessionStore struct {
	sessions []SessionMetadata
}

func (m *mockSessionStore) ListSessions() ([]SessionMetadata, error) {
	return m.sessions, nil
}

func (m *mockSessionStore) LoadSession(id string) ([]Turn, error) {
	return nil, nil
}

func (m *mockSessionStore) SaveSession(id string, turns []Turn) error {
	return nil
}

func (m *mockSessionStore) GetSessionMetadata(id string) (SessionMetadata, error) {
	return SessionMetadata{}, nil
}
