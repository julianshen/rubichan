package team

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTeamConfigDefaults(t *testing.T) {
	cfg := NewTeamConfig("alpha", "/tmp/ws")
	require.Equal(t, "alpha", cfg.TeamName)
	require.Equal(t, "/tmp/ws", cfg.WorkspaceDir)
	require.Equal(t, 10, cfg.MaxTeammates)
}

func TestTeamConfigDirs(t *testing.T) {
	cfg := NewTeamConfig("alpha", "/tmp/ws")
	require.Contains(t, cfg.TeammatesDir(), ".claude/teams/alpha")
	require.Contains(t, cfg.InboxesDir(), "inboxes")
}

func TestTeamConfigEnsureDirs(t *testing.T) {
	dir := t.TempDir()
	cfg := NewTeamConfig("alpha", dir)
	require.NoError(t, cfg.EnsureDirs())
	require.DirExists(t, cfg.TeammatesDir())
	require.DirExists(t, cfg.InboxesDir())
}

func TestAssignColorDeterministic(t *testing.T) {
	c1 := AssignColor("alice")
	c2 := AssignColor("alice")
	require.Equal(t, c1, c2)
	require.NotEmpty(t, c1)
}

func TestNewTeammateID(t *testing.T) {
	id1 := NewTeammateID("explore")
	id2 := NewTeammateID("explore")
	require.NotEqual(t, id1.AgentID, id2.AgentID)
	require.Equal(t, "explore", id1.AgentName)
	require.NotEmpty(t, id1.Color)
}

func TestTeamRegistryRegisterAndGet(t *testing.T) {
	r := NewTeamRegistry("alpha")
	id := NewTeammateID("explore")
	r.Register(id)

	got, ok := r.Get(id.AgentID)
	require.True(t, ok)
	require.Equal(t, id, got)

	got, ok = r.GetByName("explore")
	require.True(t, ok)
	require.Equal(t, id, got)
}

func TestTeamRegistryRemove(t *testing.T) {
	r := NewTeamRegistry("alpha")
	id := NewTeammateID("explore")
	r.Register(id)
	r.Remove(id.AgentID)

	_, ok := r.Get(id.AgentID)
	require.False(t, ok)
	_, ok = r.GetByName("explore")
	require.False(t, ok)
}

func TestTeamRegistryList(t *testing.T) {
	r := NewTeamRegistry("alpha")
	r.Register(NewTeammateID("a"))
	r.Register(NewTeammateID("b"))

	list := r.List()
	require.Len(t, list, 2)
}

func TestTeamRegistryIsTeammate(t *testing.T) {
	r := NewTeamRegistry("alpha")
	id := NewTeammateID("explore")
	r.Register(id)
	require.True(t, r.IsTeammate(id.AgentID))
	require.False(t, r.IsTeammate("tm-99-other"))
}
