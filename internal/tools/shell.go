package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/tools/sandbox"
)

// maxShellTimeout is the maximum allowed per-command timeout (10 minutes).
const maxShellTimeout = 600000

// shellInput represents the input for the shell tool.
type shellInput struct {
	Command      string `json:"command"`
	IsBackground bool   `json:"is_background,omitempty"`
	Timeout      int    `json:"timeout,omitempty"`     // milliseconds, capped at maxShellTimeout
	Directory    string `json:"directory,omitempty"`   // absolute path; defaults to workDir
	Description  string `json:"description,omitempty"` // human-readable explanation
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

// commandSubstitutionPattern detects $(...), `...`, <(...), and >(...) patterns
// that could enable command injection attacks. Plain variable expansion ($VAR)
// is intentionally allowed.
var commandSubstitutionPattern = regexp.MustCompile(`\$\(|` + "`" + `|<\(|>\(`)

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
	workDir        string
	timeout        time.Duration
	diffTracker    *DiffTracker
	sandbox        ShellSandbox
	processManager *ProcessManager
	sandboxCfg     config.SandboxConfig
	domainProxy    *sandbox.DomainProxy // nil when not configured
}

// NewShellTool creates a new ShellTool that runs commands in the given
// working directory with the specified timeout.
func NewShellTool(workDir string, timeout time.Duration) *ShellTool {
	return &ShellTool{
		workDir: workDir,
		timeout: timeout,
		sandbox: NewDefaultShellSandbox(workDir),
	}
}

// SetDiffTracker attaches a DiffTracker to record file changes detected
// by running git status --porcelain after command execution.
func (s *ShellTool) SetDiffTracker(dt *DiffTracker) {
	s.diffTracker = dt
}

// SetSandbox attaches an OS-level sandbox wrapper to shell executions.
func (s *ShellTool) SetSandbox(sb ShellSandbox) {
	s.sandbox = sb
}

// SetSandboxConfig attaches sandbox configuration and an optional domain proxy.
func (s *ShellTool) SetSandboxConfig(cfg config.SandboxConfig, proxy *sandbox.DomainProxy) {
	s.sandboxCfg = cfg
	s.domainProxy = proxy

	// If a proxy is running, rebuild the sandbox backend with a policy that
	// includes the proxy port. Without this, the sandbox blocks the proxy
	// (Seatbelt won't allow network-outbound, bwrap won't add --share-net).
	if proxy != nil && proxy.Port() > 0 && s.sandbox != nil {
		cfg.Network.ProxyPort = proxy.Port()
		policy := BuildSandboxPolicy(s.workDir, cfg)
		s.sandbox = NewShellSandboxWithPolicy(s.workDir, policy)
	}
}

// Sandbox returns the attached OS-level sandbox, or nil if none is set.
func (s *ShellTool) Sandbox() ShellSandbox { return s.sandbox }

// SetProcessManager attaches a ProcessManager for background execution support.
func (s *ShellTool) SetProcessManager(pm *ProcessManager) {
	s.processManager = pm
}

func (s *ShellTool) Name() string {
	return "shell"
}

func (s *ShellTool) Description() string {
	return "Execute shell commands. Commands are run via sh -c with a configurable timeout.\n" +
		"Example: {\"command\": \"npm install\", \"timeout\": 60000}"
}

func (s *ShellTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {
				"type": "string",
				"description": "The shell command to execute"
			},
			"is_background": {
				"type": "boolean",
				"description": "Run the command in the background, returning immediately with a process ID"
			},
			"timeout": {
				"type": "integer",
				"description": "Timeout in milliseconds (max 600000). Defaults to the tool-level timeout"
			},
			"directory": {
				"type": "string",
				"description": "Absolute path to use as working directory. Defaults to the project root"
			},
			"description": {
				"type": "string",
				"description": "Human-readable explanation of what this command does"
			}
		},
		"required": ["command"]
	}`)
}

func (s *ShellTool) Execute(ctx context.Context, input json.RawMessage) (ToolResult, error) {
	return s.ExecuteStream(ctx, input, nil)
}

// ExecuteStream executes shell commands while emitting incremental output.
func (s *ShellTool) ExecuteStream(ctx context.Context, input json.RawMessage, emit ToolEventEmitter) (ToolResult, error) {
	var in shellInput
	if err := json.Unmarshal(input, &in); err != nil {
		res := ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}
		emitToolEvent(emit, ToolEvent{Stage: EventEnd, Content: res.Content, IsError: true})
		return res, nil
	}
	// Validate and resolve the working directory.
	workDir := s.workDir
	if in.Directory != "" {
		if in.Directory == "." {
			in.Directory = s.workDir
		}
		if !filepath.IsAbs(in.Directory) {
			res := ToolResult{Content: "directory must be an absolute path", IsError: true}
			emitToolEvent(emit, ToolEvent{Stage: EventEnd, Content: res.Content, IsError: true})
			return res, nil
		}
		if _, statErr := os.Stat(in.Directory); statErr != nil {
			res := ToolResult{Content: fmt.Sprintf("directory does not exist: %s", in.Directory), IsError: true}
			emitToolEvent(emit, ToolEvent{Stage: EventEnd, Content: res.Content, IsError: true})
			return res, nil
		}
		// Ensure directory is within or equal to the project workDir to prevent
		// path traversal attacks that bypass recursive-rm safety checks.
		// Resolve symlinks to prevent lexical-only prefix checks from being
		// bypassed by symlinks pointing outside the project root.
		resolvedDir, evalErr := filepath.EvalSymlinks(in.Directory)
		if evalErr != nil {
			res := ToolResult{Content: fmt.Sprintf("directory cannot be resolved: %s", evalErr), IsError: true}
			emitToolEvent(emit, ToolEvent{Stage: EventEnd, Content: res.Content, IsError: true})
			return res, nil
		}
		resolvedWork, evalErr := filepath.EvalSymlinks(s.workDir)
		if evalErr != nil {
			resolvedWork = filepath.Clean(s.workDir)
		}
		if resolvedDir != resolvedWork && !strings.HasPrefix(resolvedDir, resolvedWork+string(filepath.Separator)) {
			res := ToolResult{Content: fmt.Sprintf("directory must be within the project root (%s)", s.workDir), IsError: true}
			emitToolEvent(emit, ToolEvent{Stage: EventEnd, Content: res.Content, IsError: true})
			return res, nil
		}
		workDir = resolvedDir
	}

	// Run security interceptor BEFORE any execution (including background).
	interception := inspectShellCommand(in.Command, workDir)
	if interception.routeReason != "" {
		res := ToolResult{
			Content: fmt.Sprintf("command requires routing: %s. Use the file tool for this operation.", interception.routeReason),
			IsError: true,
		}
		emitToolEvent(emit, ToolEvent{Stage: EventEnd, Content: res.Content, IsError: true})
		return res, nil
	}
	if interception.blockReason != "" {
		res := ToolResult{
			Content: fmt.Sprintf("command blocked: %s. Use the file tool for file edits.", interception.blockReason),
			IsError: true,
		}
		emitToolEvent(emit, ToolEvent{Stage: EventEnd, Content: res.Content, IsError: true})
		return res, nil
	}

	// Handle background execution (after security checks).
	if in.IsBackground {
		if s.processManager == nil {
			res := ToolResult{Content: "background execution requires a process manager", IsError: true}
			emitToolEvent(emit, ToolEvent{Stage: EventEnd, Content: res.Content, IsError: true})
			return res, nil
		}
		// Background execution does not support per-command directory or timeout
		// overrides — the ProcessManager runs in the project root with its own
		// lifecycle. Reject these combinations to avoid silent misuse.
		if in.Directory != "" {
			res := ToolResult{Content: "directory override is not supported with background execution", IsError: true}
			emitToolEvent(emit, ToolEvent{Stage: EventEnd, Content: res.Content, IsError: true})
			return res, nil
		}
		if in.Timeout > 0 {
			res := ToolResult{Content: "timeout override is not supported with background execution", IsError: true}
			emitToolEvent(emit, ToolEvent{Stage: EventEnd, Content: res.Content, IsError: true})
			return res, nil
		}
		id, output, bgErr := s.processManager.Exec(ctx, in.Command)
		if bgErr != nil {
			res := ToolResult{Content: fmt.Sprintf("background exec failed: %s", bgErr), IsError: true}
			emitToolEvent(emit, ToolEvent{Stage: EventEnd, Content: res.Content, IsError: true})
			return res, nil
		}
		content := fmt.Sprintf("process_id: %s\n%s", id, output)
		res := ToolResult{Content: content}
		emitToolEvent(emit, ToolEvent{Stage: EventEnd, Content: content})
		return res, nil
	}

	// Resolve the effective timeout.
	timeout := s.timeout
	if in.Timeout > 0 {
		ms := in.Timeout
		if ms > maxShellTimeout {
			ms = maxShellTimeout
		}
		timeout = time.Duration(ms) * time.Millisecond
	}

	// Capture a baseline of dirty paths before execution so we only attribute
	// genuinely new changes to this command, not pre-existing dirty files.
	var baseline map[string]bool
	if s.diffTracker != nil {
		baseline = s.captureBaseline()
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, "sh", "-c", in.Command)
	cmd.Dir = workDir
	excluded := IsExcludedFromSandbox(in.Command, s.sandboxCfg.ExcludedCommands)
	if s.sandbox != nil && !excluded {
		if err := s.sandbox.Wrap(cmd); err != nil {
			if isSandboxUnavailableError(err) {
				if !s.sandboxCfg.IsAllowUnsandboxedCommands() {
					res := ToolResult{Content: "sandbox unavailable and unsandboxed execution is disabled", IsError: true}
					emitToolEvent(emit, ToolEvent{Stage: EventEnd, Content: res.Content, IsError: true})
					return res, nil
				}
				// Per-command fallback — do NOT set s.sandbox = nil
				// Fall through to execute unsandboxed
			} else {
				// Policy deny or other hard error
				res := withInterceptionWarnings(ToolResult{
					Content: fmt.Sprintf("tool execution error: sandbox: %s", err.Error()),
					IsError: true,
				}, interception.warnings)
				emitToolEvent(emit, ToolEvent{Stage: EventEnd, Content: res.Display(), IsError: true})
				return res, nil
			}
		}
	}
	emitToolEvent(emit, ToolEvent{Stage: EventBegin, Content: in.Command})
	if len(interception.warnings) > 0 {
		emitToolEvent(emit, ToolEvent{Stage: EventDelta, Content: formatInterceptionWarnings(interception.warnings)})
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		res := withInterceptionWarnings(ToolResult{
			Content: fmt.Sprintf("tool execution error: %s", err.Error()),
			IsError: true,
		}, interception.warnings)
		emitToolEvent(emit, ToolEvent{Stage: EventEnd, Content: res.Display(), IsError: true})
		return res, nil
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		res := withInterceptionWarnings(ToolResult{
			Content: fmt.Sprintf("tool execution error: %s", err.Error()),
			IsError: true,
		}, interception.warnings)
		emitToolEvent(emit, ToolEvent{Stage: EventEnd, Content: res.Display(), IsError: true})
		return res, nil
	}
	if err := cmd.Start(); err != nil {
		res := withInterceptionWarnings(ToolResult{
			Content: fmt.Sprintf("tool execution error: %s", err.Error()),
			IsError: true,
		}, interception.warnings)
		emitToolEvent(emit, ToolEvent{Stage: EventEnd, Content: res.Display(), IsError: true})
		return res, nil
	}

	var (
		allOutput bytes.Buffer
		outMu     sync.Mutex
	)
	streamPipe := func(r io.Reader, isErr bool, wg *sync.WaitGroup) {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, readErr := r.Read(buf)
			if n > 0 {
				chunk := string(buf[:n])
				outMu.Lock()
				_, _ = allOutput.Write(buf[:n])
				outMu.Unlock()
				emitToolEvent(emit, ToolEvent{Stage: EventDelta, Content: chunk, IsError: isErr})
			}
			if readErr != nil {
				return
			}
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go streamPipe(stdout, false, &wg)
	go streamPipe(stderr, true, &wg)

	waitErr := cmd.Wait()
	wg.Wait()
	output := allOutput.Bytes()

	// Detect file changes regardless of exit code or timeout — a command can
	// modify files before timing out or exiting non-zero.
	if s.diffTracker != nil {
		s.detectChanges(baseline)
	}

	// Check if the timeout context (not the parent) triggered a deadline exceeded
	if timeoutCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
		res := withInterceptionWarnings(ToolResult{
			Content: fmt.Sprintf("command timed out after %s", timeout),
			IsError: true,
		}, interception.warnings)
		emitToolEvent(emit, ToolEvent{Stage: EventEnd, Content: res.Display(), IsError: true})
		return res, nil
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
	if waitErr != nil {
		if strings.TrimSpace(content) == "" {
			content = waitErr.Error()
		}
		res := withInterceptionWarnings(ToolResult{Content: content, DisplayContent: displayContent, IsError: true}, interception.warnings)
		emitToolEvent(emit, ToolEvent{Stage: EventEnd, Content: res.Display(), IsError: true})
		return res, nil
	}

	res := withInterceptionWarnings(ToolResult{Content: content, DisplayContent: displayContent}, interception.warnings)
	emitToolEvent(emit, ToolEvent{Stage: EventEnd, Content: res.Display()})
	return res, nil
}

func isSandboxUnavailableError(err error) bool {
	if err == nil {
		return false
	}

	// These strings indicate sandbox runtime/setup failures (for example
	// macOS seatbelt permission errors) where fallback to unsandboxed execution
	// is expected. Policy-deny failures (for example bubblewrap policy blocks)
	// are intentionally not matched and should remain hard failures.
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "sandbox unavailable") ||
		strings.Contains(msg, "sandbox_apply: operation not permitted") ||
		strings.Contains(msg, "operation not permitted")
}

// IsExcludedFromSandbox checks if the lead command in fullCommand matches any excluded name.
func IsExcludedFromSandbox(fullCommand string, excluded []string) bool {
	if len(excluded) == 0 {
		return false
	}
	segments := splitAllShellSegments(fullCommand)
	if len(segments) == 0 {
		return false
	}
	exe := extractExecutableName(segments[0])
	for _, ex := range excluded {
		if exe == ex {
			return true
		}
	}
	return false
}

func extractExecutableName(segment string) string {
	name, args := parseCommandExecutable(segment)
	for isCommandPrefixWrapper(name) && len(args) > 0 {
		name, args = parseCommandExecutable(strings.Join(args, " "))
	}
	return filepath.Base(name)
}

func inspectShellCommand(command, workDir string) commandInterception {
	out := commandInterception{}

	if containsCommandSubstitution(command) {
		out.blockReason = "command substitution ($(), ``, <(), >()) is blocked for security"
		return out
	}

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

// readOnlyCommands is the set of command executables considered safe (read-only).
// These commands do not modify the filesystem and can bypass approval dialogs.
var readOnlyCommands = map[string]bool{
	"ls": true, "cat": true, "head": true, "tail": true,
	"find": true, "grep": true, "rg": true, "ag": true,
	"wc": true, "pwd": true, "echo": true, "env": true,
	"which": true, "whoami": true, "date": true, "uname": true,
	"file": true, "stat": true, "du": true, "df": true,
	"tree": true, "less": true, "more": true, "diff": true,
	"sort": true, "uniq": true, "tr": true, "cut": true,
	"awk": true, "id": true, "printenv": true, "type": true,
	"test": true, "[": true, "true": true, "false": true,
	"printf": true, "basename": true, "dirname": true,
	"realpath": true, "readlink": true, "sha256sum": true,
	"md5sum": true, "xxd": true, "hexdump": true,
	"ps": true, "top": true, "uptime": true, "free": true,
	"go":  true, // go commands are checked separately for sub-commands
	"git": true, // git commands are checked separately for sub-commands
}

// readOnlyGitSubCommands is the set of git sub-commands considered read-only.
var readOnlyGitSubCommands = map[string]bool{
	"status": true, "log": true, "diff": true, "show": true,
	"branch": false, "tag": false, "remote": false, "describe": true,
	"rev-parse": true, "rev-list": true, "ls-files": true,
	"ls-tree": true, "ls-remote": true, "blame": true,
	"shortlog": true, "stash": false, "config": false,
}

// readOnlyGoSubCommands is the set of go sub-commands considered read-only.
var readOnlyGoSubCommands = map[string]bool{
	"version": true, "env": true, "list": true, "doc": true,
	"vet": true, "tool": true,
}

// IsReadOnlyCommand determines whether a shell command is read-only (safe).
// Read-only commands do not modify the filesystem and can bypass user approval.
// It splits on all shell separators: pipes (|), semicolons (;), && and ||.
func IsReadOnlyCommand(command string) bool {
	segments := splitAllShellSegments(command)
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		if !isSegmentReadOnly(seg) {
			return false
		}
	}
	return true
}

func isSegmentReadOnly(segment string) bool {
	// Output redirection (> or >>) means the command writes to a file.
	if containsOutputRedirection(segment) {
		return false
	}

	exe, args := parseCommandExecutable(segment)
	if exe == "" {
		return true
	}

	// Handle command prefix wrappers (env, command, sudo).
	if isCommandPrefixWrapper(exe) {
		// Rebuild the remaining command without the prefix and env vars.
		start := 0
		for start < len(args) {
			if strings.Contains(args[start], "=") && !strings.HasPrefix(args[start], "=") {
				start++
				continue
			}
			break
		}
		if start >= len(args) {
			return true
		}
		return isSegmentReadOnly(strings.Join(args[start:], " "))
	}

	// Check git sub-commands.
	if exe == "git" && len(args) > 0 {
		subCmd := args[0]
		if ro, known := readOnlyGitSubCommands[subCmd]; known {
			return ro
		}
		return false
	}

	// Check go sub-commands.
	if exe == "go" && len(args) > 0 {
		subCmd := args[0]
		if ro, known := readOnlyGoSubCommands[subCmd]; known {
			return ro
		}
		return false
	}

	return readOnlyCommands[exe]
}

// splitAllShellSegments splits a command on all shell separators: |, ;, &&, ||.
// It respects single and double quotes to avoid splitting inside quoted strings.
func splitAllShellSegments(command string) []string {
	var segments []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	runes := []rune(command)

	for i := 0; i < len(runes); i++ {
		r := runes[i]
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
		if inSingle || inDouble {
			current.WriteRune(r)
			continue
		}
		// Check for two-character separators first: && and ||
		if i+1 < len(runes) {
			next := runes[i+1]
			if (r == '&' && next == '&') || (r == '|' && next == '|') {
				segments = append(segments, current.String())
				current.Reset()
				i++ // skip the second character
				continue
			}
		}
		// Single-character separators: | and ;
		if r == '|' || r == ';' {
			segments = append(segments, current.String())
			current.Reset()
			continue
		}
		current.WriteRune(r)
	}
	if current.Len() > 0 {
		segments = append(segments, current.String())
	}
	return segments
}

// containsOutputRedirection checks whether a command segment contains shell
// output redirection (> or >>), respecting quotes.
func containsOutputRedirection(segment string) bool {
	inSingle := false
	inDouble := false
	escaped := false
	runes := []rune(segment)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
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
		if inSingle || inDouble {
			continue
		}
		if r == '>' {
			return true
		}
	}
	return false
}

// containsCommandSubstitution checks whether the command string contains
// shell command substitution patterns that could be used for injection.
// It scans the raw command text for $(), backticks, <(), and >() patterns.
func containsCommandSubstitution(command string) bool {
	return commandSubstitutionPattern.MatchString(command)
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
		args = append(args, fields[idx+1:]...)
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
	prefix := formatInterceptionWarnings(warnings)
	result.Content = prefix + result.Content
	if result.DisplayContent != "" {
		result.DisplayContent = prefix + result.DisplayContent
	}
	return result
}

func formatInterceptionWarnings(warnings []string) string {
	var b strings.Builder
	b.WriteString("warning: shell safety interceptor detected file-modifying pattern(s):\n")
	for _, warning := range warnings {
		b.WriteString("- ")
		b.WriteString(warning)
		b.WriteByte('\n')
	}
	return b.String()
}

func emitToolEvent(emit ToolEventEmitter, ev ToolEvent) {
	if emit == nil {
		return
	}
	emit(ev)
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
