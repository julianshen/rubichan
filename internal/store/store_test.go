package store

import (
	"testing"

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
