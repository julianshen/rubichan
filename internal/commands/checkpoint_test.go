package commands_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/checkpoint"
	"github.com/julianshen/rubichan/internal/commands"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUndoCommand(t *testing.T) {
	rootDir := t.TempDir()
	testFile := filepath.Join(rootDir, "main.go")
	os.WriteFile(testFile, []byte("original"), 0644)

	mgr, _ := checkpoint.New(rootDir, "cmd-undo", 0)
	defer func() { _ = mgr.Cleanup() }()
	mgr.Capture(context.Background(), "main.go", 1, "write")
	os.WriteFile(testFile, []byte("modified"), 0644)

	cmd := commands.NewUndoCommand(mgr)
	assert.Equal(t, "undo", cmd.Name())

	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Reverted")

	data, _ := os.ReadFile(testFile)
	assert.Equal(t, "original", string(data))
}

func TestUndoCommandEmptyStack(t *testing.T) {
	rootDir := t.TempDir()
	mgr, _ := checkpoint.New(rootDir, "cmd-undo-empty", 0)
	defer func() { _ = mgr.Cleanup() }()

	cmd := commands.NewUndoCommand(mgr)
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "No checkpoints")
}

func TestRewindCommand(t *testing.T) {
	rootDir := t.TempDir()
	testFile := filepath.Join(rootDir, "a.go")
	os.WriteFile(testFile, []byte("original"), 0644)

	mgr, _ := checkpoint.New(rootDir, "cmd-rewind", 0)
	defer func() { _ = mgr.Cleanup() }()

	mgr.Capture(context.Background(), "a.go", 1, "write")
	os.WriteFile(testFile, []byte("turn1"), 0644)
	mgr.Capture(context.Background(), "a.go", 2, "patch")
	os.WriteFile(testFile, []byte("turn2"), 0644)

	cmd := commands.NewRewindCommand(mgr)
	assert.Equal(t, "rewind", cmd.Name())

	result, err := cmd.Execute(context.Background(), []string{"0"})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Reverted")

	data, _ := os.ReadFile(testFile)
	assert.Equal(t, "original", string(data))
}

func TestRewindCommandMissingArg(t *testing.T) {
	rootDir := t.TempDir()
	mgr, _ := checkpoint.New(rootDir, "cmd-rewind-noarg", 0)
	defer func() { _ = mgr.Cleanup() }()

	cmd := commands.NewRewindCommand(mgr)
	_, err := cmd.Execute(context.Background(), nil)
	assert.Error(t, err)
}

func TestUndoCommandNilManager(t *testing.T) {
	cmd := commands.NewUndoCommand(nil)
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "not available")
}
