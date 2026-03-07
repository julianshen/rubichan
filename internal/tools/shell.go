package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// shellInput represents the input for the shell tool.
type shellInput struct {
	Command string `json:"command"`
}

type commandInterceptorAction int

const (
	interceptWarn commandInterceptorAction = iota
	interceptBlock
	interceptRouteToFileTool
)

type commandInterceptorRule struct {
	pattern *regexp.Regexp
	action  commandInterceptorAction
	message string
}

type commandInterception struct {
	blockReason string
	routeReason string
	warnings    []string
}

var defaultShellInterceptionRules = []commandInterceptorRule{
	{
		pattern: regexp.MustCompile(`^$`), // handled via token-aware parser in inspectShellCommand
		action:  interceptRouteToFileTool,
		message: "apply_patch shell commands must be routed through the file tool",
	},
	{
		pattern: regexp.MustCompile(`(?i)\b(?:echo|cat)\b[^;\n]*\s(?:>>?)\s*[^\s;]+`),
		action:  interceptWarn,
		message: "command redirects output to a file",
	},
	{
		pattern: regexp.MustCompile(`(?i)\bsed\b[^;\n]*\s-i(?:\s|$)`),
		action:  interceptWarn,
		message: "command uses sed -i for in-place file edits",
	},
	{
		pattern: regexp.MustCompile(`^$`), // handled via path-aware parser in inspectShellCommand
		action:  interceptBlock,
		message: "recursive rm is blocked by shell safety interceptor",
	},
	{
		pattern: regexp.MustCompile(`(?i)\b(?:chmod|chown)\b`),
		action:  interceptWarn,
		message: "command changes file ownership/permissions",
	},
	{
		pattern: regexp.MustCompile(`(?i)\b(?:mv|cp)\b[^;\n]*(?:\s/\S+|\s\.\./\S+)`),
		action:  interceptWarn,
		message: "command may move/copy files outside the working directory",
	},
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
	interception := inspectShellCommand(in.Command, s.workDir)
	if interception.routeReason != "" {
		return ToolResult{
			Content: fmt.Sprintf("command requires routing: %s. Use the file tool for this operation.", interception.routeReason),
			IsError: true,
		}, nil
	}
	if interception.blockReason != "" {
		return ToolResult{
			Content: fmt.Sprintf("command blocked: %s. Use the file tool for file edits.", interception.blockReason),
			IsError: true,
		}, nil
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
		return withInterceptionWarnings(ToolResult{
			Content: fmt.Sprintf("command timed out after %s", s.timeout),
			IsError: true,
		}, interception.warnings), nil
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
		return withInterceptionWarnings(ToolResult{Content: content, DisplayContent: displayContent, IsError: true}, interception.warnings), nil
	}

	return withInterceptionWarnings(ToolResult{Content: content, DisplayContent: displayContent}, interception.warnings), nil
}

// ExecuteStream implements StreamingTool. It streams stdout/stderr
// line-by-line as EventDelta events during command execution.
func (s *ShellTool) ExecuteStream(ctx context.Context, input json.RawMessage, emit func(ToolEvent)) (ToolResult, error) {
	var in shellInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	interception := inspectShellCommand(in.Command, s.workDir)
	if interception.routeReason != "" {
		return ToolResult{
			Content: fmt.Sprintf("command requires routing: %s. Use the file tool for this operation.", interception.routeReason),
			IsError: true,
		}, nil
	}
	if interception.blockReason != "" {
		return ToolResult{
			Content: fmt.Sprintf("command blocked: %s. Use the file tool for file edits.", interception.blockReason),
			IsError: true,
		}, nil
	}

	var baseline map[string]bool
	if s.diffTracker != nil {
		baseline = s.captureBaseline()
	}

	emit(ToolEvent{Stage: EventBegin, Content: fmt.Sprintf("$ %s\n", in.Command)})

	timeoutCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, "sh", "-c", in.Command)
	cmd.Dir = s.workDir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to create stdout pipe: %s", err), IsError: true}, nil
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to create stderr pipe: %s", err), IsError: true}, nil
	}

	if err := cmd.Start(); err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to start command: %s", err), IsError: true}, nil
	}

	var output strings.Builder
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text() + "\n"
			output.WriteString(line)
			emit(ToolEvent{Stage: EventDelta, Content: line})
		}
	}()
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text() + "\n"
			output.WriteString(line)
			emit(ToolEvent{Stage: EventDelta, Content: line, IsError: true})
		}
	}()

	wg.Wait()
	cmdErr := cmd.Wait()

	if s.diffTracker != nil {
		s.detectChanges(baseline)
	}

	content := output.String()
	var displayContent string

	if timeoutCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
		emit(ToolEvent{Stage: EventEnd, Content: fmt.Sprintf("timed out after %s", s.timeout), IsError: true})
		return withInterceptionWarnings(ToolResult{
			Content: fmt.Sprintf("command timed out after %s", s.timeout),
			IsError: true,
		}, interception.warnings), nil
	}

	if len(content) > maxOutputBytes {
		displayContent = content
		if len(displayContent) > maxDisplayBytes {
			displayContent = displayContent[:maxDisplayBytes] + "\n... output truncated"
		}
		content = content[:maxOutputBytes] + "\n... output truncated"
	}

	isError := cmdErr != nil
	emit(ToolEvent{Stage: EventEnd, Content: "", IsError: isError})

	return withInterceptionWarnings(ToolResult{
		Content: content, DisplayContent: displayContent, IsError: isError,
	}, interception.warnings), nil
}

func inspectShellCommand(command, workDir string) commandInterception {
	out := commandInterception{}

	if containsCommandToken(command, "apply_patch") {
		out.routeReason = "apply_patch shell commands must be routed through the file tool"
	}
	if outsideTargets := findRecursiveRMOutsideWorkdir(command, workDir); len(outsideTargets) > 0 {
		out.blockReason = fmt.Sprintf("recursive rm target(s) escape working directory: %s", strings.Join(outsideTargets, ", "))
	}

	for _, rule := range defaultShellInterceptionRules {
		if !rule.pattern.MatchString(command) {
			continue
		}
		switch rule.action {
		case interceptWarn:
			out.warnings = append(out.warnings, rule.message)
		case interceptBlock, interceptRouteToFileTool:
			if out.blockReason == "" && out.routeReason == "" {
				out.blockReason = rule.message
			}
		}
	}
	return out
}

func containsCommandToken(command, want string) bool {
	segments := splitShellSegments(command)
	for _, segment := range segments {
		exe, args := parseCommandExecutable(segment)
		if exe == "" {
			continue
		}
		if exe == want {
			return true
		}
		if isShellWrapper(exe) && len(args) >= 2 {
			// Handle nested shell executions like: sh -c "..."
			for i := 0; i < len(args)-1; i++ {
				if strings.HasPrefix(args[i], "-") && strings.Contains(args[i], "c") {
					if containsCommandToken(args[i+1], want) {
						return true
					}
					break
				}
			}
		}
		if isCommandPrefixWrapper(exe) {
			for i := 0; i < len(args); i++ {
				n := args[i]
				if strings.Contains(n, "=") && !strings.HasPrefix(n, "=") {
					continue
				}
				joined := strings.Join(args[i:], " ")
				if containsCommandToken(joined, want) {
					return true
				}
				break
			}
		}
	}
	return false
}

func findRecursiveRMOutsideWorkdir(command, workDir string) []string {
	var outside []string
	segments := splitShellSegments(command)

	for _, segment := range segments {
		exe, fields := parseCommandExecutable(segment)
		if exe == "" {
			continue
		}
		if isShellWrapper(exe) && len(fields) >= 2 {
			for i := 0; i < len(fields)-1; i++ {
				if strings.HasPrefix(fields[i], "-") && strings.Contains(fields[i], "c") {
					outside = append(outside, findRecursiveRMOutsideWorkdir(fields[i+1], workDir)...)
					break
				}
			}
			continue
		}
		if isCommandPrefixWrapper(exe) {
			start := 0
			for start < len(fields) {
				n := fields[start]
				if strings.Contains(n, "=") && !strings.HasPrefix(n, "=") {
					start++
					continue
				}
				break
			}
			if start < len(fields) {
				outside = append(outside, findRecursiveRMOutsideWorkdir(strings.Join(fields[start:], " "), workDir)...)
			}
			continue
		}
		if exe != "rm" {
			continue
		}

		recursive := false
		targets := make([]string, 0, len(fields))
		parseTargets := false
		for _, token := range fields {
			if token == "" {
				continue
			}
			if parseTargets {
				targets = append(targets, token)
				continue
			}
			if token == "--" {
				parseTargets = true
				continue
			}
			if strings.HasPrefix(token, "--") {
				if token == "--recursive" {
					recursive = true
				}
				continue
			}
			if strings.HasPrefix(token, "-") {
				if strings.Contains(token, "r") {
					recursive = true
				}
				continue
			}
			targets = append(targets, token)
		}

		if !recursive {
			continue
		}
		for _, target := range targets {
			if isOutsideWorkdir(target, workDir) {
				outside = append(outside, target)
			}
		}
	}

	return outside
}

func splitShellSegments(command string) []string {
	var segments []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false

	flush := func() {
		part := strings.TrimSpace(current.String())
		if part != "" {
			segments = append(segments, part)
		}
		current.Reset()
	}

	for _, r := range command {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			current.WriteRune(r)
			escaped = true
			continue
		}
		if r == '\'' && !inDouble {
			inSingle = !inSingle
			current.WriteRune(r)
			continue
		}
		if r == '"' && !inSingle {
			inDouble = !inDouble
			current.WriteRune(r)
			continue
		}
		if !inSingle && !inDouble && (r == ';' || r == '\n' || r == '\r') {
			flush()
			continue
		}
		current.WriteRune(r)
	}
	flush()
	return segments
}

func parseCommandExecutable(segment string) (string, []string) {
	fields := parseShellWords(segment)
	if len(fields) == 0 {
		return "", nil
	}
	idx := 0
	for idx < len(fields) {
		n := fields[idx]
		if strings.Contains(n, "=") && !strings.HasPrefix(n, "=") {
			idx++
			continue
		}
		exe := n
		args := make([]string, 0, len(fields)-(idx+1))
		for _, field := range fields[idx+1:] {
			args = append(args, field)
		}
		return exe, args
	}
	return "", nil
}

func parseShellWords(s string) []string {
	var out []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false

	flush := func() {
		if current.Len() == 0 {
			return
		}
		out = append(out, current.String())
		current.Reset()
	}

	for _, r := range s {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && !inSingle {
			escaped = true
			continue
		}
		if r == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if r == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if !inSingle && !inDouble && (r == ' ' || r == '\t') {
			flush()
			continue
		}
		current.WriteRune(r)
	}
	flush()
	return out
}

func isShellWrapper(exe string) bool {
	switch exe {
	case "sh", "bash", "zsh", "/bin/sh", "/bin/bash", "/bin/zsh":
		return true
	default:
		return false
	}
}

func isCommandPrefixWrapper(exe string) bool {
	switch exe {
	case "env", "command", "sudo":
		return true
	default:
		return false
	}
}

func isOutsideWorkdir(target, workDir string) bool {
	if target == "" || target == "-" {
		return false
	}
	if strings.HasPrefix(target, "~") {
		return true
	}

	var absTarget string
	if filepath.IsAbs(target) {
		absTarget = filepath.Clean(target)
	} else {
		absTarget = filepath.Clean(filepath.Join(workDir, target))
	}
	absWorkDir := filepath.Clean(workDir)

	return !strings.HasPrefix(absTarget, absWorkDir+string(filepath.Separator)) && absTarget != absWorkDir
}

func withInterceptionWarnings(result ToolResult, warnings []string) ToolResult {
	if len(warnings) == 0 {
		return result
	}
	var b strings.Builder
	b.WriteString("warning: shell safety interceptor detected file-modifying pattern(s):\n")
	for _, warning := range warnings {
		b.WriteString("- ")
		b.WriteString(warning)
		b.WriteByte('\n')
	}
	prefix := b.String()

	result.Content = prefix + result.Content
	if result.DisplayContent != "" {
		result.DisplayContent = prefix + result.DisplayContent
	}
	return result
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
