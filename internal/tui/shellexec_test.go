package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExecuteShellCommand_Success(t *testing.T) {
	r := ExecuteShellCommand("echo hello")
	assert.Equal(t, "hello", r.Output)
	assert.Equal(t, 0, r.ExitCode)
	assert.False(t, r.IsError)
	assert.Equal(t, "echo hello", r.Command)
}

func TestExecuteShellCommand_Error(t *testing.T) {
	r := ExecuteShellCommand("exit 1")
	assert.True(t, r.IsError)
	assert.Equal(t, 1, r.ExitCode)
}

func TestExecuteShellCommand_NotFound(t *testing.T) {
	r := ExecuteShellCommand("nonexistent_command_xyz_123")
	assert.True(t, r.IsError)
	assert.True(t, strings.Contains(r.Output, "not found"))
}

func TestExecuteShellCommand_PipesWork(t *testing.T) {
	r := ExecuteShellCommand("echo hello | tr a-z A-Z")
	assert.Equal(t, "HELLO", r.Output)
	assert.False(t, r.IsError)
}

func TestExecuteShellCommand_EmptyOutput(t *testing.T) {
	r := ExecuteShellCommand("true")
	assert.Equal(t, "", r.Output)
	assert.False(t, r.IsError)
}
