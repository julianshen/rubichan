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

	// Capture a baseline of dirty paths before execution so we only attribute
	// genuinely new changes to this command, not pre-existing dirty files.
	var baseline map[string]bool
	if s.diffTracker != nil {
		baseline = s.captureBaseline()
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, "sh", "-c", in.Command)
	cmd.Dir = s.workDir

	output, err := cmd.CombinedOutput()

	// Detect file changes regardless of exit code or timeout — a command can
	// modify files before timing out or exiting non-zero.
	if s.diffTracker != nil {
		s.detectChanges(baseline)
	}

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

	// Non-zero exit code
	if err != nil {
		return ToolResult{Content: content, DisplayContent: displayContent, IsError: true}, nil
	}

	return ToolResult{Content: content, DisplayContent: displayContent}, nil
}

// detectChangesTimeout caps how long git status may run inside detectChanges.
// A short, fixed timeout prevents blocking the agent loop on large repos.
const detectChangesTimeout = 2 * time.Second

// captureBaseline runs git status --porcelain and returns the set of currently
// dirty paths. This is called before command execution so that detectChanges
// can distinguish genuinely new changes from pre-existing dirty files.
func (s *ShellTool) captureBaseline() map[string]bool {
	timeoutCtx, cancel := context.WithTimeout(context.Background(), detectChangesTimeout)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, "git", "status", "--porcelain")
	cmd.Dir = s.workDir
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return nil
	}

	paths := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if len(line) < 4 {
			continue
		}
		statusCode := line[:2]
		path := line[3:]
		if statusCode[0] == 'R' || statusCode[0] == 'C' {
			if parts := strings.SplitN(path, " -> ", 2); len(parts) == 2 {
				path = parts[1]
			}
		}
		paths[path] = true
	}
	return paths
}

// detectChanges runs git status --porcelain to find files modified by the
// shell command and records them in the DiffTracker. It uses porcelain format
// to capture staged, unstaged, and untracked changes. Paths that existed in
// the pre-execution baseline or were already recorded this turn are skipped
// to avoid attributing pre-existing dirty files to the current command.
//
// A dedicated timeout is used so that a slow git status cannot block the
// agent indefinitely, independent of the parent context's deadline.
func (s *ShellTool) detectChanges(baseline map[string]bool) {
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

	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if len(line) < 4 {
			continue
		}
		// Porcelain format: XY <space> path
		// For renames/copies: XY <space> orig_path -> new_path
		// X = index status, Y = worktree status
		statusCode := line[:2]
		path := line[3:]

		// For renames/copies, extract the destination (new) path.
		if statusCode[0] == 'R' || statusCode[0] == 'C' {
			if parts := strings.SplitN(path, " -> ", 2); len(parts) == 2 {
				path = parts[1]
			}
		}

		// Skip paths already recorded in this turn.
		if existing[path] {
			continue
		}

		// Skip paths that were dirty before this command ran.
		if baseline[path] {
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
