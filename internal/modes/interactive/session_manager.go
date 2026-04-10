package interactive

import (
	"fmt"
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
	for i := 0; i < len(sessions)-1; i++ {
		for j := i + 1; j < len(sessions); j++ {
			if sessions[j].CreatedAt.After(sessions[i].CreatedAt) {
				sessions[i], sessions[j] = sessions[j], sessions[i]
			}
		}
	}

	return sessions, nil
}

// Load retrieves a session's turn history.
func (sm *SessionManager) Load(id string) ([]Turn, error) {
	turns, err := sm.store.LoadSession(id)
	if err != nil {
		return nil, fmt.Errorf("load session %s: %w", id, err)
	}
	return turns, nil
}
