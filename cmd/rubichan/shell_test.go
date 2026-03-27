package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShellCmdExists(t *testing.T) {
	cmd := shellCmd()
	require.NotNil(t, cmd)
	assert.Equal(t, "shell", cmd.Use)
	assert.Contains(t, cmd.Short, "interactive shell")

	// Verify help text renders without error.
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--help"})
	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "AI-enhanced interactive shell")
}

func TestShellCmdInheritsGlobalFlags(t *testing.T) {
	// shellCmd itself doesn't define flags — it relies on the root command's
	// persistent flags (--model, --provider, --auto-approve, --resume).
	// Verify the command is correctly structured for subcommand usage.
	cmd := shellCmd()
	assert.Equal(t, "shell", cmd.Use)
	assert.True(t, cmd.SilenceUsage)
	assert.True(t, cmd.SilenceErrors)
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
