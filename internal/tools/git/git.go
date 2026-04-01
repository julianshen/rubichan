package gittools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/julianshen/rubichan/internal/tools"
)

const commandTimeout = 30 * time.Second

type statusInput struct {
	Path      string `json:"path,omitempty"`
	Untracked *bool  `json:"untracked,omitempty"`
}

type diffInput struct {
	Path   string `json:"path,omitempty"`
	Staged bool   `json:"staged,omitempty"`
	Base   string `json:"base,omitempty"`
	Head   string `json:"head,omitempty"`
}

type logInput struct {
	Path   string `json:"path,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Author string `json:"author,omitempty"`
}

type showInput struct {
	Rev string `json:"rev"`
}

type blameInput struct {
	Path      string `json:"path"`
	LineStart int    `json:"line_start,omitempty"`
	LineEnd   int    `json:"line_end,omitempty"`
}

type gitTool struct {
	workDir     string
	name        string
	description string
	schema      json.RawMessage
	run         func(context.Context, *gitTool, json.RawMessage) (tools.ToolResult, error)
}

// NewStatusTool returns a read-only git status tool.
func NewStatusTool(workDir string) tools.Tool {
	return &gitTool{
		workDir:     workDir,
		name:        "git_status",
		description: "Show git working tree status. Use this instead of shell 'git status'.",
		schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Optional path filter"},
				"untracked": {"type": "boolean", "description": "Include untracked files (default true)"}
			}
		}`),
		run: runStatus,
	}
}

// NewDiffTool returns a read-only git diff tool.
func NewDiffTool(workDir string) tools.Tool {
	return &gitTool{
		workDir:     workDir,
		name:        "git_diff",
		description: "Show a git diff. Use this instead of shell 'git diff'.",
		schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Optional path filter"},
				"staged": {"type": "boolean", "description": "Show staged diff"},
				"base": {"type": "string", "description": "Optional base revision"},
				"head": {"type": "string", "description": "Optional head revision"}
			}
		}`),
		run: runDiff,
	}
}

// NewLogTool returns a read-only git log tool.
func NewLogTool(workDir string) tools.Tool {
	return &gitTool{
		workDir:     workDir,
		name:        "git_log",
		description: "Show git commit history. Use this instead of shell 'git log'.",
		schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Optional path filter"},
				"limit": {"type": "integer", "description": "Maximum number of commits (default 10)"},
				"author": {"type": "string", "description": "Optional author filter"}
			}
		}`),
		run: runLog,
	}
}

// NewShowTool returns a read-only git show tool.
func NewShowTool(workDir string) tools.Tool {
	return &gitTool{
		workDir:     workDir,
		name:        "git_show",
		description: "Show a git revision's content. Use this instead of shell 'git show'.",
		schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"rev": {"type": "string", "description": "Revision to show"}
			},
			"required": ["rev"]
		}`),
		run: runShow,
	}
}

// NewBlameTool returns a read-only git blame tool.
func NewBlameTool(workDir string) tools.Tool {
	return &gitTool{
		workDir:     workDir,
		name:        "git_blame",
		description: "Show git blame for a file. Use this instead of shell 'git blame'.",
		schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "File path to blame"},
				"line_start": {"type": "integer", "description": "Optional start line"},
				"line_end": {"type": "integer", "description": "Optional end line"}
			},
			"required": ["path"]
		}`),
		run: runBlame,
	}
}

// Name returns the tool name.
func (t *gitTool) Name() string { return t.name }

// Description returns a human-readable description of the tool.
func (t *gitTool) Description() string { return t.description }

// InputSchema returns the JSON schema for the tool's input.
func (t *gitTool) InputSchema() json.RawMessage { return t.schema }

// Execute runs the git command and returns the output.
func (t *gitTool) Execute(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	return t.run(ctx, t, input)
}

func runStatus(ctx context.Context, t *gitTool, input json.RawMessage) (tools.ToolResult, error) {
	var in statusInput
	if err := json.Unmarshal(input, &in); err != nil {
		return errResult("invalid input: %s", err), nil
	}
	repoRoot, err := repoRoot(ctx, t.workDir)
	if err != nil {
		return errResult("%s", err), nil
	}

	args := []string{"status", "--short"}
	if in.Untracked != nil && !*in.Untracked {
		args = append(args, "--untracked-files=no")
	}
	if in.Path != "" {
		repoPath, pathErr := resolveRepoPath(repoRoot, t.workDir, in.Path)
		if pathErr != nil {
			return errResult("%s", pathErr), nil
		}
		args = append(args, "--", repoPath)
	}
	return runGit(ctx, repoRoot, args...)
}

func runDiff(ctx context.Context, t *gitTool, input json.RawMessage) (tools.ToolResult, error) {
	var in diffInput
	if err := json.Unmarshal(input, &in); err != nil {
		return errResult("invalid input: %s", err), nil
	}
	if in.Head != "" && in.Base == "" {
		return errResult("base is required when head is provided"), nil
	}
	if in.Staged && (in.Base != "" || in.Head != "") {
		return errResult("staged cannot be combined with base/head"), nil
	}
	if err := rejectLeadingDash("base", in.Base); err != nil {
		return errResult("%s", err), nil
	}
	if err := rejectLeadingDash("head", in.Head); err != nil {
		return errResult("%s", err), nil
	}
	repoRoot, err := repoRoot(ctx, t.workDir)
	if err != nil {
		return errResult("%s", err), nil
	}

	args := []string{"diff"}
	if in.Staged {
		args = append(args, "--cached")
	}
	if in.Base != "" {
		args = append(args, in.Base)
	}
	if in.Head != "" {
		args = append(args, in.Head)
	}
	if in.Path != "" {
		repoPath, pathErr := resolveRepoPath(repoRoot, t.workDir, in.Path)
		if pathErr != nil {
			return errResult("%s", pathErr), nil
		}
		args = append(args, "--", repoPath)
	}
	return runGit(ctx, repoRoot, args...)
}

func runLog(ctx context.Context, t *gitTool, input json.RawMessage) (tools.ToolResult, error) {
	var in logInput
	if err := json.Unmarshal(input, &in); err != nil {
		return errResult("invalid input: %s", err), nil
	}
	repoRoot, err := repoRoot(ctx, t.workDir)
	if err != nil {
		return errResult("%s", err), nil
	}

	limit := in.Limit
	if limit <= 0 {
		limit = 10
	}
	if err := rejectLeadingDash("author", in.Author); err != nil {
		return errResult("%s", err), nil
	}
	args := []string{"log", "--date=short", "--pretty=format:%H %ad %an %s", "--max-count", strconv.Itoa(limit)}
	if in.Author != "" {
		args = append(args, "--author", in.Author)
	}
	if in.Path != "" {
		repoPath, pathErr := resolveRepoPath(repoRoot, t.workDir, in.Path)
		if pathErr != nil {
			return errResult("%s", pathErr), nil
		}
		args = append(args, "--", repoPath)
	}
	return runGit(ctx, repoRoot, args...)
}

func runShow(ctx context.Context, t *gitTool, input json.RawMessage) (tools.ToolResult, error) {
	var in showInput
	if err := json.Unmarshal(input, &in); err != nil {
		return errResult("invalid input: %s", err), nil
	}
	if strings.TrimSpace(in.Rev) == "" {
		return errResult("rev is required"), nil
	}
	if err := rejectLeadingDash("rev", in.Rev); err != nil {
		return errResult("%s", err), nil
	}
	repoRoot, err := repoRoot(ctx, t.workDir)
	if err != nil {
		return errResult("%s", err), nil
	}
	return runGit(ctx, repoRoot, "show", "--stat", "--format=fuller", in.Rev)
}

func runBlame(ctx context.Context, t *gitTool, input json.RawMessage) (tools.ToolResult, error) {
	var in blameInput
	if err := json.Unmarshal(input, &in); err != nil {
		return errResult("invalid input: %s", err), nil
	}
	if strings.TrimSpace(in.Path) == "" {
		return errResult("path is required"), nil
	}
	repoRoot, err := repoRoot(ctx, t.workDir)
	if err != nil {
		return errResult("%s", err), nil
	}
	repoPath, pathErr := resolveRepoPath(repoRoot, t.workDir, in.Path)
	if pathErr != nil {
		return errResult("%s", pathErr), nil
	}

	args := []string{"blame", "--date=short"}
	if in.LineStart > 0 {
		end := in.LineEnd
		if end <= 0 {
			end = in.LineStart
		}
		if end < in.LineStart {
			return errResult("line_end must be greater than or equal to line_start"), nil
		}
		args = append(args, "-L", fmt.Sprintf("%d,%d", in.LineStart, end))
	}
	args = append(args, "--", repoPath)
	return runGit(ctx, repoRoot, args...)
}

func repoRoot(ctx context.Context, workDir string) (string, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(timeoutCtx, "git", "-C", workDir, "rev-parse", "--show-toplevel")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("not in a git repository: %s", strings.TrimSpace(string(out)))
	}
	root := strings.TrimSpace(string(out))
	resolved, resolveErr := filepath.EvalSymlinks(root)
	if resolveErr == nil {
		return resolved, nil
	}
	return root, nil
}

func runGit(ctx context.Context, repoRoot string, args ...string) (tools.ToolResult, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmdArgs := append([]string{"-C", repoRoot}, args...)
	cmd := exec.CommandContext(timeoutCtx, "git", cmdArgs...)
	out, err := cmd.CombinedOutput()
	content := strings.TrimRight(string(out), "\n")
	content, display := truncate(content)
	if err != nil {
		return tools.ToolResult{Content: content, DisplayContent: display, IsError: true}, nil
	}
	if content == "" {
		return tools.ToolResult{Content: "<empty>"}, nil
	}
	return tools.ToolResult{Content: content, DisplayContent: display}, nil
}

func truncate(s string) (string, string) {
	const maxContent = 30 * 1024
	const maxDisplay = 100 * 1024
	if len(s) <= maxContent {
		return s, ""
	}
	display := s
	if len(display) > maxDisplay {
		display = display[:maxDisplay] + "\n... output truncated"
	}
	return s[:maxContent] + "\n... output truncated", display
}

func resolveRepoPath(repoRoot, workDir, inputPath string) (string, error) {
	var abs string
	if filepath.IsAbs(inputPath) {
		abs = filepath.Clean(inputPath)
	} else {
		abs = filepath.Join(workDir, inputPath)
	}
	abs, err := filepath.Abs(abs)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	resolvedRepo := repoRoot
	if evalRepo, evalErr := filepath.EvalSymlinks(repoRoot); evalErr == nil {
		resolvedRepo = evalRepo
	}
	resolvedAbs := abs
	if evalAbs, evalErr := filepath.EvalSymlinks(abs); evalErr == nil {
		resolvedAbs = evalAbs
	} else if os.IsNotExist(evalErr) {
		resolvedAbs = abs
	}
	if !strings.HasPrefix(resolvedAbs, resolvedRepo+string(filepath.Separator)) && resolvedAbs != resolvedRepo {
		return "", fmt.Errorf("path must stay within the git repository")
	}
	rel, err := filepath.Rel(resolvedRepo, resolvedAbs)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	return filepath.ToSlash(rel), nil
}

func errResult(format string, args ...any) tools.ToolResult {
	return tools.ToolResult{Content: fmt.Sprintf(format, args...), IsError: true}
}

func rejectLeadingDash(name, value string) error {
	if value != "" && strings.HasPrefix(value, "-") {
		return fmt.Errorf("%s must not start with '-'", name)
	}
	return nil
}
