package main

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/internal/shell"
)

func TestShellCmdExists(t *testing.T) {
	cmd := shellCmd()
	require.NotNil(t, cmd)
	assert.Equal(t, "shell", cmd.Use)
	assert.Contains(t, cmd.Short, "interactive shell")

	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--help"})
	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "AI-enhanced interactive shell")
}

func TestShellCmdHelpContainsPrefixDocs(t *testing.T) {
	cmd := shellCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--help"})
	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "!")
	assert.Contains(t, output, "?")
	assert.Contains(t, output, "$PATH")
}

func TestMakeSlashCommandFunc(t *testing.T) {
	t.Parallel()

	registry := commands.NewRegistry()
	require.NoError(t, registry.Register(commands.NewQuitCommand()))
	require.NoError(t, registry.Register(commands.NewHelpCommand(registry)))

	fn := makeSlashCommandFunc(registry)

	// Unknown command
	output, quit, err := fn(context.Background(), "nonexistent", nil)
	require.NoError(t, err)
	assert.False(t, quit)
	assert.Contains(t, output, "unknown command")

	// Quit command
	_, quit, err = fn(context.Background(), "quit", nil)
	require.NoError(t, err)
	assert.True(t, quit)

	// Help command
	output, quit, err = fn(context.Background(), "help", nil)
	require.NoError(t, err)
	assert.False(t, quit)
	assert.NotEmpty(t, output)
}

func TestErrExitIsExported(t *testing.T) {
	t.Parallel()

	// Verify ErrExit is usable with errors.Is
	err := shell.ErrExit
	assert.True(t, errors.Is(err, shell.ErrExit))
}
