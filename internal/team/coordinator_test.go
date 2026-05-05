package team

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/require"
)

type mockSpawner struct {
	spawned []agentsdk.SpawnRequest
	err     error
}

func (m *mockSpawner) Spawn(ctx context.Context, req agentsdk.SpawnRequest) error {
	if m.err != nil {
		return m.err
	}
	m.spawned = append(m.spawned, req)
	return nil
}

func TestCoordinatorSpawnTeammate(t *testing.T) {
	dir := t.TempDir()
	cfg := NewTeamConfig("alpha", dir)
	spawner := &mockSpawner{}
	coord, err := NewCoordinator(cfg, spawner)
	require.NoError(t, err)

	tid, err := coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "explore"})
	require.NoError(t, err)
	require.NotNil(t, tid)
	require.Equal(t, "explore", tid.AgentName)
	require.Len(t, spawner.spawned, 1)
}

func TestCoordinatorSpawnDuplicate(t *testing.T) {
	dir := t.TempDir()
	cfg := NewTeamConfig("alpha", dir)
	spawner := &mockSpawner{}
	coord, _ := NewCoordinator(cfg, spawner)

	_, err := coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "explore"})
	require.NoError(t, err)

	_, err = coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "explore"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")
}

func TestCoordinatorSpawnMax(t *testing.T) {
	dir := t.TempDir()
	cfg := NewTeamConfig("alpha", dir)
	cfg.MaxTeammates = 1
	spawner := &mockSpawner{}
	coord, _ := NewCoordinator(cfg, spawner)

	_, err := coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "a"})
	require.NoError(t, err)

	_, err = coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "b"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "max teammates")
}

func TestCoordinatorSendMessage(t *testing.T) {
	dir := t.TempDir()
	cfg := NewTeamConfig("alpha", dir)
	spawner := &mockSpawner{}
	coord, _ := NewCoordinator(cfg, spawner)

	_, _ = coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "alice"})
	_, _ = coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "bob"})

	err := coord.SendMessage("alice", "bob", "hello")
	require.NoError(t, err)

	msgs, err := coord.Mailbox().Read("bob")
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.Equal(t, "hello", msgs[0].Text)
}

func TestCoordinatorSendMessageSelf(t *testing.T) {
	dir := t.TempDir()
	cfg := NewTeamConfig("alpha", dir)
	spawner := &mockSpawner{}
	coord, _ := NewCoordinator(cfg, spawner)
	_, _ = coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "alice"})

	err := coord.SendMessage("alice", "alice", "hello")
	require.Error(t, err)
	require.Contains(t, err.Error(), "self")
}

func TestCoordinatorBroadcast(t *testing.T) {
	dir := t.TempDir()
	cfg := NewTeamConfig("alpha", dir)
	spawner := &mockSpawner{}
	coord, _ := NewCoordinator(cfg, spawner)

	_, _ = coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "alice"})
	_, _ = coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "bob"})
	_, _ = coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "carol"})

	err := coord.SendMessage("alice", "*", "all hands")
	require.NoError(t, err)

	bobMsgs, _ := coord.Mailbox().Read("bob")
	carolMsgs, _ := coord.Mailbox().Read("carol")
	require.Len(t, bobMsgs, 1)
	require.Len(t, carolMsgs, 1)
	require.Equal(t, "all hands", bobMsgs[0].Text)
}

func TestCoordinatorShutdownTeammate(t *testing.T) {
	dir := t.TempDir()
	cfg := NewTeamConfig("alpha", dir)
	spawner := &mockSpawner{}
	coord, _ := NewCoordinator(cfg, spawner)
	_, _ = coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "bob"})

	err := coord.ShutdownTeammate("bob", "alice")
	require.NoError(t, err)

	msgs, _ := coord.Mailbox().Read("bob")
	require.Len(t, msgs, 1)
	require.Equal(t, agentsdk.MessageTypeShutdownRequest, msgs[0].Type)
}

func TestCoordinatorShutdownAll(t *testing.T) {
	dir := t.TempDir()
	cfg := NewTeamConfig("alpha", dir)
	spawner := &mockSpawner{}
	coord, _ := NewCoordinator(cfg, spawner)

	_, _ = coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "alice"})
	_, _ = coord.SpawnTeammate(context.Background(), agentsdk.SpawnRequest{AgentName: "bob"})

	err := coord.ShutdownAll("alice")
	require.NoError(t, err)

	msgs, _ := coord.Mailbox().Read("bob")
	require.Len(t, msgs, 1)
	require.Equal(t, agentsdk.MessageTypeShutdownRequest, msgs[0].Type)
}
