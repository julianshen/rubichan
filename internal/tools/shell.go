package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// shellInput represents the input for the shell tool.
type shellInput struct {
	Command string `json:"command"`
}

// ShellTool executes shell commands with timeout and output truncation.
type ShellTool struct {
	workDir     string
	timeout     time.Duration
	diffTracker *DiffTracker
}

// NewShellTool creates a new ShellTool that runs commands in the given
// working directory with the specified timeout.
func NewShellTool(workDir string, timeout time.Duration) *ShellTool {
	return &ShellTool{
		workDir: workDir,
		timeout: timeout,
	}
}

// SetDiffTracker attaches a DiffTracker to record file changes detected
// by running git status --porcelain after command execution.
func (s *ShellTool) SetDiffTracker(dt *DiffTracker) {
	s.diffTracker = dt
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

	// Truncate output for LLM; optionally set richer DisplayContent for user.
	content := string(output)
	var displayContent string
	if len(output) > maxOutputBytes {
		content = string(output[:maxOutputBytes]) + "\n... output truncated"
		// Give the user more output via DisplayContent.
		if len(output) > maxDisplayBytes {
			displayContent = string(output[:maxDisplayBytes]) + "\n... output truncated"
		} else {
			displayContent = string(output)
		}
	}

	// Detect file changes regardless of exit code — a command can modify
	// files and still exit non-zero (e.g., "echo x > file && false").
	if s.diffTracker != nil {
		s.detectChanges(ctx)
	}

	// Non-zero exit code
	if err != nil {
		return ToolResult{Content: content, DisplayContent: displayContent, IsError: true}, nil
	}

	return ToolResult{Content: content, DisplayContent: displayContent}, nil
}

// detectChangesTimeout caps how long git status may run inside detectChanges.
// A short, fixed timeout prevents blocking the agent loop on large repos.
const detectChangesTimeout = 2 * time.Second

// detectChanges runs git status --porcelain to find files modified by the
// shell command and records them in the DiffTracker. It uses porcelain format
// to capture staged, unstaged, and untracked changes. Already-recorded paths
// are skipped to avoid duplicates across multiple shell invocations per turn.
//
// A dedicated timeout is used so that a slow git status cannot block the
// agent indefinitely, independent of the parent context's deadline.
func (s *ShellTool) detectChanges(_ context.Context) {
	timeoutCtx, cancel := context.WithTimeout(context.Background(), detectChangesTimeout)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, "git", "status", "--porcelain")
	cmd.Dir = s.workDir
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return
	}

	// Build a set of paths already recorded in this turn to avoid duplicates.
	existing := make(map[string]bool)
	for _, c := range s.diffTracker.Changes() {
		existing[c.Path] = true
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if len(line) < 4 {
			continue
		}
		// Porcelain format: XY <space> path
		// For renames/copies: XY <space> new_path -> old_path
		// X = index status, Y = worktree status
		statusCode := line[:2]
		path := line[3:]

		// For renames/copies, extract the destination (new) path.
		if statusCode[0] == 'R' || statusCode[0] == 'C' {
			if parts := strings.SplitN(path, " -> ", 2); len(parts) == 2 {
				path = parts[1]
			}
		}

		if existing[path] {
			continue
		}

		var op Operation
		switch {
		case statusCode == "??" || statusCode[0] == 'A' || statusCode[1] == 'A':
			op = OpCreated
		case statusCode[0] == 'R' || statusCode[0] == 'C':
			op = OpCreated
		case statusCode[0] == 'D' || statusCode[1] == 'D':
			op = OpDeleted
		default:
			op = OpModified
		}
		s.diffTracker.Record(FileChange{
			Path:      path,
			Operation: op,
			Tool:      "shell",
		})
		existing[path] = true
	}
}
