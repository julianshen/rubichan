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
	assert.Contains(t, result.Output, "abc-1234")
	assert.Contains(t, result.Output, "Fix auth")
	assert.Contains(t, result.Output, "forked from abc-1234")
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
