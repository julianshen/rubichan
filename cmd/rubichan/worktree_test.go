package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWorktreeCmd_Structure(t *testing.T) {
	cmd := worktreeCmd()
	assert.Equal(t, "worktree", cmd.Use)

	// Verify subcommands exist.
	names := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	assert.Contains(t, names, "list")
	assert.Contains(t, names, "remove")
	assert.Contains(t, names, "cleanup")
}

func TestWorktreeRemoveCmd_RequiresArg(t *testing.T) {
	cmd := worktreeRemoveCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 1 arg")
}

func TestWorktreeRemoveCmd_HasForceFlag(t *testing.T) {
	cmd := worktreeRemoveCmd()
	f := cmd.Flags().Lookup("force")
	assert.NotNil(t, f)
	assert.Equal(t, "false", f.DefValue)
}
