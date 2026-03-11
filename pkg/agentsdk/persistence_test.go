package agentsdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSessionInfoFields(t *testing.T) {
	s := SessionInfo{
		ID:         "sess_123",
		Title:      "Debug session",
		Model:      "claude-sonnet-4-5",
		WorkingDir: "/home/user/project",
	}
	assert.Equal(t, "sess_123", s.ID)
	assert.Equal(t, "Debug session", s.Title)
}

func TestStoredMessageFields(t *testing.T) {
	m := StoredMessage{
		SessionID: "sess_123",
		Seq:       0,
		Role:      "user",
		Content: []ContentBlock{
			{Type: "text", Text: "hello"},
		},
	}
	assert.Equal(t, "user", m.Role)
	assert.Len(t, m.Content, 1)
	assert.Equal(t, "hello", m.Content[0].Text)
}

// mockPersistenceStore verifies the interface can be implemented.
type mockPersistenceStore struct{}

func (m *mockPersistenceStore) CreateSession(_, _, _ string) error { return nil }
func (m *mockPersistenceStore) GetSession(_ string) (*SessionInfo, error) {
	return &SessionInfo{ID: "test"}, nil
}
func (m *mockPersistenceStore) UpdateSessionTitle(_, _ string) error                     { return nil }
func (m *mockPersistenceStore) AppendMessage(_ string, _ string, _ []ContentBlock) error { return nil }
func (m *mockPersistenceStore) GetMessages(_ string) ([]StoredMessage, error)            { return nil, nil }
func (m *mockPersistenceStore) SaveSnapshot(_ string, _ []Message, _ int) error          { return nil }
func (m *mockPersistenceStore) GetSnapshot(_ string) ([]Message, error)                  { return nil, nil }
func (m *mockPersistenceStore) SaveBlob(_, _, _, _ string, _ int) error                  { return nil }
func (m *mockPersistenceStore) GetBlob(_ string) (string, error)                         { return "", nil }

func TestPersistenceStoreInterfaceSatisfied(t *testing.T) {
	var store PersistenceStore = &mockPersistenceStore{}
	sess, err := store.GetSession("test")
	assert.NoError(t, err)
	assert.Equal(t, "test", sess.ID)
}
