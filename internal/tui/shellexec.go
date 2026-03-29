package tui

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

const shellExecTimeout = 30 * time.Second

// ShellExecResult holds the output of a direct shell command.
type ShellExecResult struct {
	Command  string
	Output   string
	ExitCode int
	IsError  bool
}

// ExecuteShellCommand runs a command via sh -c and captures combined output.
// It enforces a 30-second timeout to prevent runaway commands from blocking
// the TUI indefinitely.
func ExecuteShellCommand(command string) ShellExecResult {
	ctx, cancel := context.WithTimeout(context.Background(), shellExecTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	out, err := cmd.CombinedOutput()

	result := ShellExecResult{
		Command: command,
		Output:  strings.TrimRight(string(out), "\n"),
	}

	if err != nil {
		result.IsError = true
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			result.Output = result.Output + "\n[command timed out after 30s]"
			result.ExitCode = -1
		}
	}

	return result
}
