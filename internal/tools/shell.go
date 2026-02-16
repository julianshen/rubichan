package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

const maxOutputBytes = 30 * 1024

// shellInput represents the input for the shell tool.
type shellInput struct {
	Command string `json:"command"`
}

// ShellTool executes shell commands with timeout and output truncation.
type ShellTool struct {
	workDir string
	timeout time.Duration
}

// NewShellTool creates a new ShellTool that runs commands in the given
// working directory with the specified timeout.
func NewShellTool(workDir string, timeout time.Duration) *ShellTool {
	return &ShellTool{
		workDir: workDir,
		timeout: timeout,
	}
}

func (s *ShellTool) Name() string {
	return "shell"
}

func (s *ShellTool) Description() string {
	return "Execute shell commands. Commands are run via sh -c with a configurable timeout."
}

func (s *ShellTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {
				"type": "string",
				"description": "The shell command to execute"
			}
		},
		"required": ["command"]
	}`)
}

func (s *ShellTool) Execute(ctx context.Context, input json.RawMessage) (ToolResult, error) {
	var in shellInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, "sh", "-c", in.Command)
	cmd.Dir = s.workDir

	output, err := cmd.CombinedOutput()

	// Check if the timeout context (not the parent) triggered a deadline exceeded
	if timeoutCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
		return ToolResult{
			Content: fmt.Sprintf("command timed out after %s", s.timeout),
			IsError: true,
		}, nil
	}

	// Truncate output if it exceeds the maximum
	content := string(output)
	if len(output) > maxOutputBytes {
		content = string(output[:maxOutputBytes]) + "\n... output truncated"
	}

	// Non-zero exit code
	if err != nil {
		return ToolResult{Content: content, IsError: true}, nil
	}

	return ToolResult{Content: content}, nil
}
