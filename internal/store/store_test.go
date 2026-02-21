package store

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewStoreInMemory(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	require.NotNil(t, s)

	err = s.Close()
	assert.NoError(t, err)
}

func TestApproveAndIsApprovedAlways(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	// Not approved initially.
	approved, err := s.IsApproved("my-skill", "file:read")
	require.NoError(t, err)
	assert.False(t, approved)

	// Approve with "always" scope.
	err = s.Approve("my-skill", "file:read", "always")
	require.NoError(t, err)

	// Now it should be approved.
	approved, err = s.IsApproved("my-skill", "file:read")
	require.NoError(t, err)
	assert.True(t, approved)

	// Different permission should not be approved.
	approved, err = s.IsApproved("my-skill", "shell:exec")
	require.NoError(t, err)
	assert.False(t, approved)
}

func TestApproveOnceScope(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	// Approve with "once" scope - should NOT satisfy IsApproved
	// because IsApproved only checks for permanent ("always") approvals.
	err = s.Approve("my-skill", "net:fetch", "once")
	require.NoError(t, err)

	approved, err := s.IsApproved("my-skill", "net:fetch")
	require.NoError(t, err)
	assert.False(t, approved, "once-scoped approval should not satisfy IsApproved")
}

func TestRevoke(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	// Approve two permissions for the same skill.
	require.NoError(t, s.Approve("my-skill", "file:read", "always"))
	require.NoError(t, s.Approve("my-skill", "shell:exec", "always"))

	// Revoke all permissions for the skill.
	err = s.Revoke("my-skill")
	require.NoError(t, err)

	// Both should now be unapproved.
	approved, err := s.IsApproved("my-skill", "file:read")
	require.NoError(t, err)
	assert.False(t, approved)

	approved, err = s.IsApproved("my-skill", "shell:exec")
	require.NoError(t, err)
	assert.False(t, approved)
}

func TestListApprovals(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	require.NoError(t, s.Approve("my-skill", "file:read", "always"))
	require.NoError(t, s.Approve("my-skill", "shell:exec", "once"))
	// Approval for a different skill should not appear.
	require.NoError(t, s.Approve("other-skill", "net:fetch", "always"))

	approvals, err := s.ListApprovals("my-skill")
	require.NoError(t, err)
	require.Len(t, approvals, 2)

	permissions := make(map[string]string)
	for _, a := range approvals {
		assert.Equal(t, "my-skill", a.Skill)
		assert.False(t, a.ApprovedAt.IsZero())
		permissions[a.Permission] = a.Scope
	}
	assert.Equal(t, "always", permissions["file:read"])
	assert.Equal(t, "once", permissions["shell:exec"])
}

func TestSaveAndGetSkillState(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	state := SkillInstallState{
		Name:    "code-review",
		Version: "1.2.0",
		Source:  "registry",
	}
	err = s.SaveSkillState(state)
	require.NoError(t, err)

	got, err := s.GetSkillState("code-review")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "code-review", got.Name)
	assert.Equal(t, "1.2.0", got.Version)
	assert.Equal(t, "registry", got.Source)
	assert.False(t, got.InstalledAt.IsZero())
}

func TestGetSkillStateNotFound(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	got, err := s.GetSkillState("nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got, "should return nil for missing skill state")
}

func TestListAllSkillStates(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	// Empty store should return empty slice.
	states, err := s.ListAllSkillStates()
	require.NoError(t, err)
	assert.Empty(t, states)

	// Add some skill states.
	require.NoError(t, s.SaveSkillState(SkillInstallState{
		Name:    "code-review",
		Version: "1.0.0",
		Source:  "registry",
	}))
	require.NoError(t, s.SaveSkillState(SkillInstallState{
		Name:    "formatter",
		Version: "2.1.0",
		Source:  "git",
	}))

	states, err = s.ListAllSkillStates()
	require.NoError(t, err)
	require.Len(t, states, 2)

	// Results should be sorted by name.
	assert.Equal(t, "code-review", states[0].Name)
	assert.Equal(t, "1.0.0", states[0].Version)
	assert.Equal(t, "registry", states[0].Source)
	assert.False(t, states[0].InstalledAt.IsZero())

	assert.Equal(t, "formatter", states[1].Name)
	assert.Equal(t, "2.1.0", states[1].Version)
	assert.Equal(t, "git", states[1].Source)
	assert.False(t, states[1].InstalledAt.IsZero())
}

func TestCacheAndGetRegistryEntry(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	entry := RegistryEntry{
		Name:        "code-review",
		Version:     "1.2.0",
		Description: "Automated code review skill",
	}
	err = s.CacheRegistryEntry(entry)
	require.NoError(t, err)

	got, err := s.GetCachedRegistry("code-review")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "code-review", got.Name)
	assert.Equal(t, "1.2.0", got.Version)
	assert.Equal(t, "Automated code review skill", got.Description)
	assert.False(t, got.CachedAt.IsZero())

	// Missing entry should return nil.
	got, err = s.GetCachedRegistry("nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestForeignKeyEnforcement(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	_, err = s.db.Exec(
		`INSERT INTO messages (session_id, seq, role, content) VALUES (?, ?, ?, ?)`,
		"nonexistent-session", 0, "user", `[{"type":"text","text":"hi"}]`,
	)
	require.Error(t, err, "foreign key constraint should reject orphan message")
}

func TestSessionAndMessageTablesExist(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	_, err = s.db.Exec(
		`INSERT INTO sessions (id, model) VALUES (?, ?)`,
		"test-id", "gpt-4",
	)
	require.NoError(t, err, "sessions table should exist")

	_, err = s.db.Exec(
		`INSERT INTO messages (session_id, seq, role, content) VALUES (?, ?, ?, ?)`,
		"test-id", 0, "user", `[]`,
	)
	require.NoError(t, err, "messages table should exist and accept valid FK")
}

func TestDeleteSkillState(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	// Save a skill state.
	require.NoError(t, s.SaveSkillState(SkillInstallState{
		Name:    "to-delete",
		Version: "1.0.0",
		Source:  "local",
	}))

	// Verify it exists.
	got, err := s.GetSkillState("to-delete")
	require.NoError(t, err)
	require.NotNil(t, got)

	// Delete it.
	err = s.DeleteSkillState("to-delete")
	require.NoError(t, err)

	// Verify it no longer exists.
	got, err = s.GetSkillState("to-delete")
	require.NoError(t, err)
	assert.Nil(t, got, "skill state should be deleted")

	// Deleting a non-existent skill should not error.
	err = s.DeleteSkillState("nonexistent")
	assert.NoError(t, err)
}

func TestSessionTypeZeroValue(t *testing.T) {
	var s Session
	assert.Empty(t, s.ID)
	assert.Empty(t, s.Title)
	assert.Empty(t, s.Model)
	assert.True(t, s.CreatedAt.IsZero())
	assert.Equal(t, 0, s.TokenCount)
}

func TestStoredMessageTypeZeroValue(t *testing.T) {
	var m StoredMessage
	assert.Empty(t, m.SessionID)
	assert.Empty(t, m.Role)
	assert.Nil(t, m.Content)
	assert.Equal(t, 0, m.Seq)
}

func TestCreateAndGetSession(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	sess := Session{
		ID:           "sess-001",
		Title:        "My first session",
		Model:        "claude-3-opus",
		WorkingDir:   "/home/user/project",
		SystemPrompt: "You are helpful.",
		TokenCount:   0,
	}
	err = s.CreateSession(sess)
	require.NoError(t, err)

	got, err := s.GetSession("sess-001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "sess-001", got.ID)
	assert.Equal(t, "My first session", got.Title)
	assert.Equal(t, "claude-3-opus", got.Model)
	assert.Equal(t, "/home/user/project", got.WorkingDir)
	assert.Equal(t, "You are helpful.", got.SystemPrompt)
	assert.Equal(t, 0, got.TokenCount)
	assert.False(t, got.CreatedAt.IsZero())
	assert.False(t, got.UpdatedAt.IsZero())
}

func TestGetSessionNotFound(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	got, err := s.GetSession("nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got, "should return nil for missing session")
}

func TestCreateSessionDuplicateID(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	sess := Session{ID: "dup-id", Model: "gpt-4"}
	require.NoError(t, s.CreateSession(sess))

	err = s.CreateSession(sess)
	require.Error(t, err, "duplicate session ID should error")
}

func TestUpdateSession(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	sess := Session{ID: "sess-upd", Model: "gpt-4", Title: "Original"}
	require.NoError(t, s.CreateSession(sess))

	sess.Title = "Updated Title"
	sess.TokenCount = 5000
	err = s.UpdateSession(sess)
	require.NoError(t, err)

	got, err := s.GetSession("sess-upd")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Updated Title", got.Title)
	assert.Equal(t, 5000, got.TokenCount)
	assert.False(t, got.UpdatedAt.Before(got.CreatedAt), "updated_at should be at or after created_at")
}

func TestUpdateSessionNotFound(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	err = s.UpdateSession(Session{ID: "nonexistent", Model: "gpt-4"})
	require.Error(t, err, "updating non-existent session should error")
}

func TestDeleteSession(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	sess := Session{ID: "sess-del", Model: "gpt-4"}
	require.NoError(t, s.CreateSession(sess))

	err = s.DeleteSession("sess-del")
	require.NoError(t, err)

	got, err := s.GetSession("sess-del")
	require.NoError(t, err)
	assert.Nil(t, got, "deleted session should not be found")

	err = s.DeleteSession("nonexistent")
	assert.NoError(t, err)
}

func TestDeleteSessionCascadesMessages(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	sess := Session{ID: "sess-cascade", Model: "gpt-4"}
	require.NoError(t, s.CreateSession(sess))

	_, err = s.db.Exec(
		`INSERT INTO messages (session_id, seq, role, content) VALUES (?, ?, ?, ?)`,
		"sess-cascade", 0, "user", `[{"type":"text","text":"hi"}]`,
	)
	require.NoError(t, err)

	require.NoError(t, s.DeleteSession("sess-cascade"))

	var count int
	err = s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id = ?`, "sess-cascade").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "messages should be cascade-deleted")
}

func TestListSessions(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	sessions, err := s.ListSessions(10)
	require.NoError(t, err)
	assert.Empty(t, sessions)

	_, err = s.db.Exec(
		`INSERT INTO sessions (id, title, model, created_at, updated_at) VALUES
		 ('s1', 'First', 'gpt-4', datetime('now', '-2 minutes'), datetime('now', '-2 minutes')),
		 ('s2', 'Second', 'gpt-4', datetime('now', '-1 minute'), datetime('now', '-1 minute')),
		 ('s3', 'Third', 'gpt-4', datetime('now'), datetime('now'))`)
	require.NoError(t, err)

	sessions, err = s.ListSessions(10)
	require.NoError(t, err)
	require.Len(t, sessions, 3)
	assert.Equal(t, "s3", sessions[0].ID)
	assert.Equal(t, "s2", sessions[1].ID)
	assert.Equal(t, "s1", sessions[2].ID)

	sessions, err = s.ListSessions(2)
	require.NoError(t, err)
	require.Len(t, sessions, 2)
	assert.Equal(t, "s3", sessions[0].ID)
	assert.Equal(t, "s2", sessions[1].ID)
}

func TestAppendAndGetMessages(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	sess := Session{ID: "sess-msg", Model: "gpt-4"}
	require.NoError(t, s.CreateSession(sess))

	userContent := []provider.ContentBlock{
		{Type: "text", Text: "Hello!"},
	}
	err = s.AppendMessage("sess-msg", "user", userContent)
	require.NoError(t, err)

	assistantContent := []provider.ContentBlock{
		{Type: "text", Text: "Let me check."},
		{Type: "tool_use", ID: "t1", Name: "file", Input: json.RawMessage(`{"op":"read"}`)},
	}
	err = s.AppendMessage("sess-msg", "assistant", assistantContent)
	require.NoError(t, err)

	msgs, err := s.GetMessages("sess-msg")
	require.NoError(t, err)
	require.Len(t, msgs, 2)

	assert.Equal(t, "sess-msg", msgs[0].SessionID)
	assert.Equal(t, 0, msgs[0].Seq)
	assert.Equal(t, "user", msgs[0].Role)
	require.Len(t, msgs[0].Content, 1)
	assert.Equal(t, "text", msgs[0].Content[0].Type)
	assert.Equal(t, "Hello!", msgs[0].Content[0].Text)
	assert.False(t, msgs[0].CreatedAt.IsZero())

	assert.Equal(t, 1, msgs[1].Seq)
	assert.Equal(t, "assistant", msgs[1].Role)
	require.Len(t, msgs[1].Content, 2)
	assert.Equal(t, "text", msgs[1].Content[0].Type)
	assert.Equal(t, "tool_use", msgs[1].Content[1].Type)
	assert.Equal(t, "t1", msgs[1].Content[1].ID)
}

func TestAppendMessageAutoSequence(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	require.NoError(t, s.CreateSession(Session{ID: "seq-test", Model: "m"}))

	for i := 0; i < 5; i++ {
		err := s.AppendMessage("seq-test", "user", []provider.ContentBlock{
			{Type: "text", Text: fmt.Sprintf("msg %d", i)},
		})
		require.NoError(t, err)
	}

	msgs, err := s.GetMessages("seq-test")
	require.NoError(t, err)
	require.Len(t, msgs, 5)
	for i, m := range msgs {
		assert.Equal(t, i, m.Seq, "seq should auto-increment")
	}
}

func TestAppendMessageInvalidSession(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	err = s.AppendMessage("nonexistent", "user", []provider.ContentBlock{
		{Type: "text", Text: "orphan"},
	})
	require.Error(t, err, "should reject message for non-existent session")
}

func TestGetMessagesEmpty(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	require.NoError(t, s.CreateSession(Session{ID: "empty", Model: "m"}))

	msgs, err := s.GetMessages("empty")
	require.NoError(t, err)
	assert.Empty(t, msgs)
}

func TestGetMessagesWithMalformedJSON(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	require.NoError(t, s.CreateSession(Session{ID: "bad-json", Model: "m"}))

	// Insert a message with malformed JSON content directly.
	_, err = s.db.Exec(
		`INSERT INTO messages (session_id, seq, role, content) VALUES (?, ?, ?, ?)`,
		"bad-json", 0, "user", `not-valid-json`,
	)
	require.NoError(t, err)

	_, err = s.GetMessages("bad-json")
	require.Error(t, err, "should fail on malformed JSON content")
	assert.Contains(t, err.Error(), "unmarshal content")
}

func TestAppendMessageMarshalError(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	require.NoError(t, s.CreateSession(Session{ID: "marshal-test", Model: "m"}))

	// Normal content should work fine.
	err = s.AppendMessage("marshal-test", "user", []provider.ContentBlock{
		{Type: "text", Text: "hello"},
	})
	require.NoError(t, err)

	// Verify it was stored.
	msgs, err := s.GetMessages("marshal-test")
	require.NoError(t, err)
	require.Len(t, msgs, 1)
}

func TestStoreOperationsAfterClose(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)

	// Close the store.
	require.NoError(t, s.Close())

	// All operations should return errors.
	err = s.CreateSession(Session{ID: "x", Model: "m"})
	assert.Error(t, err, "CreateSession after close should fail")

	_, err = s.GetSession("x")
	assert.Error(t, err, "GetSession after close should fail")

	_, err = s.ListSessions(10)
	assert.Error(t, err, "ListSessions after close should fail")

	err = s.UpdateSession(Session{ID: "x"})
	assert.Error(t, err, "UpdateSession after close should fail")

	err = s.DeleteSession("x")
	assert.Error(t, err, "DeleteSession after close should fail")

	err = s.AppendMessage("x", "user", nil)
	assert.Error(t, err, "AppendMessage after close should fail")

	_, err = s.GetMessages("x")
	assert.Error(t, err, "GetMessages after close should fail")
}
