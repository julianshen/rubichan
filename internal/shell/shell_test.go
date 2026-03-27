package shell

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mockShellExec(stdout, stderr string, exitCode int) ShellExecFunc {
	return func(_ context.Context, _ string, _ string) (string, string, int, error) {
		return stdout, stderr, exitCode, nil
	}
}

func mockAgentTurn(response string) AgentTurnFunc {
	return func(_ context.Context, msg string) (<-chan TurnEvent, error) {
		ch := make(chan TurnEvent, 2)
		ch <- TurnEvent{Type: "text_delta", Text: response}
		ch <- TurnEvent{Type: "done"}
		close(ch)
		return ch, nil
	}
}

func newTestHost(input string, shellExec ShellExecFunc, agentTurn AgentTurnFunc) (*ShellHost, *bytes.Buffer, *bytes.Buffer) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	host := NewShellHost(ShellHostConfig{
		WorkDir:     "/project",
		HomeDir:     "/home/user",
		Executables: map[string]bool{"echo": true, "ls": true, "go": true, "git": true},
		ShellExec:   shellExec,
		AgentTurn:   agentTurn,
		Stdin:       strings.NewReader(input),
		Stdout:      stdout,
		Stderr:      stderr,
		GitBranchFn: func(string) string { return "" },
	})

	return host, stdout, stderr
}

func TestShellHost_ShellCommandExecution(t *testing.T) {
	t.Parallel()

	var capturedCmd string
	exec := func(_ context.Context, cmd string, _ string) (string, string, int, error) {
		capturedCmd = cmd
		return "hello\n", "", 0, nil
	}

	host, stdout, _ := newTestHost("echo hello\n", exec, nil)
	err := host.Run(context.Background())
	assert.NoError(t, err)

	assert.Equal(t, "echo hello", capturedCmd)
	assert.Contains(t, stdout.String(), "hello\n")
}

func TestShellHost_LLMQueryExecution(t *testing.T) {
	t.Parallel()

	var capturedMsg string
	agent := func(_ context.Context, msg string) (<-chan TurnEvent, error) {
		capturedMsg = msg
		ch := make(chan TurnEvent, 2)
		ch <- TurnEvent{Type: "text_delta", Text: "The codebase is a Go project."}
		ch <- TurnEvent{Type: "done"}
		close(ch)
		return ch, nil
	}

	host, stdout, _ := newTestHost("explain this codebase\n", nil, agent)
	err := host.Run(context.Background())
	assert.NoError(t, err)

	assert.Equal(t, "explain this codebase", capturedMsg)
	assert.Contains(t, stdout.String(), "The codebase is a Go project.")
}

func TestShellHost_BuiltinCDChangesWorkDir(t *testing.T) {
	t.Parallel()

	// Create a real temp directory to cd into
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "src")
	require.NoError(t, os.MkdirAll(subDir, 0o755))

	var capturedDir string
	exec := func(_ context.Context, cmd string, workDir string) (string, string, int, error) {
		capturedDir = workDir
		return "", "", 0, nil
	}

	input := "cd " + subDir + "\nls\n"
	host, _, _ := newTestHost(input, exec, nil)
	host.workDir = tmpDir

	err := host.Run(context.Background())
	assert.NoError(t, err)

	assert.Equal(t, subDir, capturedDir)
}

func TestShellHost_BuiltinCDInvalidPath(t *testing.T) {
	t.Parallel()

	host, _, stderr := newTestHost("cd /nonexistent/path/xyz\n", nil, nil)
	err := host.Run(context.Background())
	assert.NoError(t, err)

	assert.Contains(t, stderr.String(), "no such directory")
	assert.Equal(t, "/project", host.workDir) // unchanged
}

func TestShellHost_BuiltinExport(t *testing.T) {
	t.Parallel()

	host, _, _ := newTestHost("export FOO=bar\n", nil, nil)
	err := host.Run(context.Background())
	assert.NoError(t, err)

	assert.Equal(t, "bar", host.env["FOO"])
}

func TestShellHost_ExitTerminates(t *testing.T) {
	t.Parallel()

	// After exit, we shouldn't see any more prompts
	host, _, _ := newTestHost("exit\necho should not run\n", mockShellExec("", "", 0), nil)
	err := host.Run(context.Background())

	assert.ErrorIs(t, err, errExit)
}

func TestShellHost_EOFTerminates(t *testing.T) {
	t.Parallel()

	// Empty reader = immediate EOF
	host, _, _ := newTestHost("", nil, nil)
	err := host.Run(context.Background())

	assert.NoError(t, err)
}

func TestShellHost_EmptyInputRendersNewPrompt(t *testing.T) {
	t.Parallel()

	// Two empty lines then EOF
	host, stdout, _ := newTestHost("\n\n", nil, nil)
	err := host.Run(context.Background())
	assert.NoError(t, err)

	// Should have rendered the prompt 3 times (2 empty + 1 before EOF fails)
	promptCount := strings.Count(stdout.String(), "ai$ ")
	assert.Equal(t, 3, promptCount)
}

func TestShellHost_ContextInjectionAfterShellCommand(t *testing.T) {
	t.Parallel()

	exec := mockShellExec("FAIL: TestFoo", "", 1)

	var capturedMsg string
	agent := func(_ context.Context, msg string) (<-chan TurnEvent, error) {
		capturedMsg = msg
		ch := make(chan TurnEvent, 2)
		ch <- TurnEvent{Type: "text_delta", Text: "The test failed because..."}
		ch <- TurnEvent{Type: "done"}
		close(ch)
		return ch, nil
	}

	// First a shell command (go test), then an LLM query
	host, _, _ := newTestHost("go test ./...\nwhy did that fail?\n", exec, agent)
	err := host.Run(context.Background())
	assert.NoError(t, err)

	// The LLM query should contain context about the previous command
	assert.Contains(t, capturedMsg, "go test ./...")
	assert.Contains(t, capturedMsg, "exit code 1")
	assert.Contains(t, capturedMsg, "FAIL: TestFoo")
	assert.Contains(t, capturedMsg, "why did that fail?")
}

func TestShellHost_SlashCommandDelegation(t *testing.T) {
	t.Parallel()

	host, stdout, _ := newTestHost("/model claude-sonnet-4-5\n", nil, nil)
	err := host.Run(context.Background())
	assert.NoError(t, err)

	assert.Contains(t, stdout.String(), "/model claude-sonnet-4-5")
}

func TestShellHost_Mode(t *testing.T) {
	t.Parallel()

	host, _, _ := newTestHost("", nil, nil)
	assert.Equal(t, "shell", host.Mode())
}
