package agentsdk

// PersistenceStore defines the storage interface for agent state.
// Implementations may use SQLite, in-memory maps, or any other backend.
// All methods must be safe for concurrent use.
type PersistenceStore interface {
	// Session management.
	CreateSession(id, model, workingDir string) error
	GetSession(id string) (*SessionInfo, error)
	UpdateSessionTitle(id, title string) error

	// Message history.
	AppendMessage(sessionID, role string, content []ContentBlock) error
	GetMessages(sessionID string) ([]StoredMessage, error)

	// Compaction snapshots (for session resume after compaction).
	SaveSnapshot(sessionID string, messages []Message, tokenCount int) error
	GetSnapshot(sessionID string) ([]Message, error)

	// Large tool result offloading.
	SaveBlob(id, sessionID, toolName, content string, byteSize int) error
	GetBlob(id string) (string, error)
}

// SessionInfo is a minimal session record returned by PersistenceStore.
type SessionInfo struct {
	ID         string
	Title      string
	Model      string
	WorkingDir string
}

// StoredMessage is a persisted message within a session.
type StoredMessage struct {
	SessionID string
	Seq       int
	Role      string
	Content   []ContentBlock
}
