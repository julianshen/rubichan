package commands_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionsCommand(t *testing.T) {
	sessions := []store.Session{
		{ID: "abc-12345", Title: "Fix auth", Model: "claude", UpdatedAt: time.Now()},
		{ID: "def-45678", Title: "Refactor", Model: "opus", UpdatedAt: time.Now(), ForkedFrom: "abc-12345"},
	}

	cmd := commands.NewSessionsCommand(func() ([]store.Session, error) {
		return sessions, nil
	})
	assert.Equal(t, "sessions", cmd.Name())

	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "abc-12345")
	assert.Contains(t, result.Output, "Fix auth")
	assert.Contains(t, result.Output, "forked from abc-1234") // forked-from stays truncated
}

func TestSessionsCommandEmpty(t *testing.T) {
	cmd := commands.NewSessionsCommand(func() ([]store.Session, error) {
		return nil, nil
	})
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "No sessions")
}

func TestSessionsCommandNilCallback(t *testing.T) {
	cmd := commands.NewSessionsCommand(nil)
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "not available")
}

func TestSessionsCommandShowsFullID(t *testing.T) {
	fullUUID := "8a5b6c0f-1234-5678-abcd-ef0123456789"
	sessions := []store.Session{
		{ID: fullUUID, Title: "Test", Model: "opus", UpdatedAt: time.Now()},
	}
	cmd := commands.NewSessionsCommand(func() ([]store.Session, error) {
		return sessions, nil
	})
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, fullUUID, "full session UUID must appear in output for --resume")
}

func TestForkCommand(t *testing.T) {
	cmd := commands.NewForkCommand(func(ctx context.Context) (string, error) {
		return "new-session-id", nil
	})
	assert.Equal(t, "fork", cmd.Name())

	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "new-session-id")
	assert.Contains(t, result.Output, "Forked")
}

func TestForkCommandNilCallback(t *testing.T) {
	cmd := commands.NewForkCommand(nil)
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "not available")
}

func TestForkCommandError(t *testing.T) {
	cmd := commands.NewForkCommand(func(ctx context.Context) (string, error) {
		return "", fmt.Errorf("no store")
	})
	_, err := cmd.Execute(context.Background(), nil)
	assert.Error(t, err)
}

func TestSessionsCommandDescription(t *testing.T) {
	cmd := commands.NewSessionsCommand(nil)
	assert.NotEmpty(t, cmd.Description())
}

func TestSessionsCommandArguments(t *testing.T) {
	cmd := commands.NewSessionsCommand(nil)
	assert.Nil(t, cmd.Arguments())
}

func TestSessionsCommandComplete(t *testing.T) {
	cmd := commands.NewSessionsCommand(nil)
	assert.Nil(t, cmd.Complete(context.Background(), nil))
}

func TestSessionsCommandError(t *testing.T) {
	cmd := commands.NewSessionsCommand(func() ([]store.Session, error) {
		return nil, fmt.Errorf("db error")
	})
	_, err := cmd.Execute(context.Background(), nil)
	assert.Error(t, err)
}

func TestSessionsCommandUntitledSession(t *testing.T) {
	sessions := []store.Session{
		{ID: "abc-12345", Title: "", Model: "claude", UpdatedAt: time.Now()},
	}
	cmd := commands.NewSessionsCommand(func() ([]store.Session, error) {
		return sessions, nil
	})
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "(untitled)")
}

func TestSessionsCommandShortForkedFromID(t *testing.T) {
	// ForkedFrom ID <= 8 chars should be shown in full.
	sessions := []store.Session{
		{ID: "abc-12345", Title: "Test", Model: "claude", UpdatedAt: time.Now(), ForkedFrom: "short"},
	}
	cmd := commands.NewSessionsCommand(func() ([]store.Session, error) {
		return sessions, nil
	})
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "forked from short")
}

func TestForkCommandDescription(t *testing.T) {
	cmd := commands.NewForkCommand(nil)
	assert.NotEmpty(t, cmd.Description())
}

func TestForkCommandArguments(t *testing.T) {
	cmd := commands.NewForkCommand(nil)
	assert.Nil(t, cmd.Arguments())
}

func TestForkCommandComplete(t *testing.T) {
	cmd := commands.NewForkCommand(nil)
	assert.Nil(t, cmd.Complete(context.Background(), nil))
}
