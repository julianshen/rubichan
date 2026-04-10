package interactive

import (
	"fmt"
	"strings"
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
	listErr  error
	loadErr  error
}

func (m *mockSessionStore) ListSessions() ([]SessionMetadata, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.sessions, nil
}

func (m *mockSessionStore) LoadSession(id string) ([]Turn, error) {
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	return []Turn{}, nil
}

func (m *mockSessionStore) SaveSession(id string, turns []Turn) error {
	return nil
}

func (m *mockSessionStore) GetSessionMetadata(id string) (SessionMetadata, error) {
	return SessionMetadata{}, nil
}

func TestSessionManagerListEmpty(t *testing.T) {
	mockStore := &mockSessionStore{sessions: []SessionMetadata{}}
	sm := NewSessionManager(mockStore)

	sessions, err := sm.List()
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestSessionManagerListError(t *testing.T) {
	mockStore := &mockSessionStore{
		listErr: fmt.Errorf("store unavailable"),
	}
	sm := NewSessionManager(mockStore)

	_, err := sm.List()
	if err == nil {
		t.Fatal("expected error from List()")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "list sessions") {
		t.Errorf("error not wrapped with context. Got: %s", errMsg)
	}
}

func TestSessionManagerLoadError(t *testing.T) {
	mockStore := &mockSessionStore{
		loadErr: fmt.Errorf("session not found"),
	}
	sm := NewSessionManager(mockStore)

	_, err := sm.Load("sess-nonexistent")
	if err == nil {
		t.Fatal("expected error from Load()")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "load session") {
		t.Errorf("error not wrapped with context. Got: %s", errMsg)
	}
}

func TestSessionManagerListSortStability(t *testing.T) {
	now := time.Now()
	mockStore := &mockSessionStore{
		sessions: []SessionMetadata{
			{ID: "sess-a", CreatedAt: now, TurnCount: 1},
			{ID: "sess-b", CreatedAt: now, TurnCount: 2}, // Same timestamp
			{ID: "sess-c", CreatedAt: now.Add(-1 * time.Hour), TurnCount: 3},
		},
	}
	sm := NewSessionManager(mockStore)

	sessions, err := sm.List()
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}

	// Latest (now) should be first 2 entries (order stable)
	if sessions[2].CreatedAt.After(now) {
		t.Errorf("expected oldest entry at index 2, got %v", sessions[2].CreatedAt)
	}
}
