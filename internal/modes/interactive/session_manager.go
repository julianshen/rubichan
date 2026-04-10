package interactive

import (
	"fmt"
	"sort"
	"time"
)

// SessionMetadata holds metadata about a saved session.
type SessionMetadata struct {
	ID        string
	CreatedAt time.Time
	TurnCount int
	Project   string // optional: extracted from session or user-provided
}

// SessionStore interface for persistence operations.
type SessionStore interface {
	ListSessions() ([]SessionMetadata, error)
	LoadSession(id string) ([]Turn, error)
	SaveSession(id string, turns []Turn) error
	GetSessionMetadata(id string) (SessionMetadata, error)
}

// Turn represents a single exchange in conversation.
type Turn struct {
	ID        string
	Timestamp time.Time
	UserInput string
	AgentResp string
}

// SessionManager wraps session operations and enforces business logic.
type SessionManager struct {
	store SessionStore
}

// NewSessionManager creates a SessionManager.
func NewSessionManager(store SessionStore) *SessionManager {
	return &SessionManager{store: store}
}

// List returns all sessions sorted by creation time (newest first).
func (sm *SessionManager) List() ([]SessionMetadata, error) {
	sessions, err := sm.store.ListSessions()
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	// Sort by CreatedAt descending (newest first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].CreatedAt.After(sessions[j].CreatedAt)
	})

	return sessions, nil
}

// Load retrieves a session's turn history by ID.
// The returned slice should not be mutated by the caller.
func (sm *SessionManager) Load(id string) ([]Turn, error) {
	turns, err := sm.store.LoadSession(id)
	if err != nil {
		return nil, fmt.Errorf("load session %s: %w", id, err)
	}
	return turns, nil
}

// ListAfter returns sessions created after the given time, sorted newest first.
func (sm *SessionManager) ListAfter(cutoff time.Time) ([]SessionMetadata, error) {
	all, err := sm.List()
	if err != nil {
		return nil, err
	}

	var filtered []SessionMetadata
	for _, s := range all {
		if s.CreatedAt.After(cutoff) {
			filtered = append(filtered, s)
		}
	}

	return filtered, nil
}
