package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/tools"
)

// GitManifest returns the skill manifest for the built-in git skill.
func GitManifest() skills.SkillManifest {
	return skills.SkillManifest{
		Name:        "git",
		Version:     "1.0.0",
		Description: "Built-in git tools for repository inspection",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Permissions: []skills.Permission{skills.PermGitRead},
	}
}

// GitBackend implements skills.SkillBackend for the built-in git skill.
type GitBackend struct {
	// WorkDir is the working directory for git commands.
	WorkDir string

	tools []tools.Tool
}

// Load creates the git-diff, git-log, and git-status tools.
func (b *GitBackend) Load(_ skills.SkillManifest, _ skills.PermissionChecker) error {
	b.tools = []tools.Tool{
		&gitDiffTool{workDir: b.WorkDir},
		&gitLogTool{workDir: b.WorkDir},
		&gitStatusTool{workDir: b.WorkDir},
	}
	return nil
}

// Tools returns the git tools created during Load.
func (b *GitBackend) Tools() []tools.Tool {
	return b.tools
}

// Hooks returns an empty map — git skill does not register any hooks.
func (b *GitBackend) Hooks() map[skills.HookPhase]skills.HookHandler {
	return nil
}

// Commands returns nil — git does not provide slash commands.
func (b *GitBackend) Commands() []commands.SlashCommand { return nil }

// Unload is a no-op for the git skill.
func (b *GitBackend) Unload() error {
	return nil
}

// --- git-diff tool ---

type gitDiffTool struct {
	workDir string
}

type gitDiffInput struct {
	Range string `json:"range"`
}

func (t *gitDiffTool) Name() string        { return "git-diff" }
func (t *gitDiffTool) Description() string { return "Show changes between commits, working tree, etc." }

func (t *gitDiffTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"range": {
				"type": "string",
				"description": "Optional diff range (e.g. HEAD~1, main..feature)"
			}
		}
	}`)
}

func (t *gitDiffTool) Execute(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	var in gitDiffInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	args := []string{"diff"}
	if in.Range != "" {
		if strings.HasPrefix(in.Range, "-") {
			return tools.ToolResult{Content: "invalid range: must not start with '-'", IsError: true}, nil
		}
		args = append(args, in.Range)
	}

	return runGit(ctx, t.workDir, args...)
}

// --- git-log tool ---

type gitLogTool struct {
	workDir string
}

type gitLogInput struct {
	Count int `json:"count"`
}

func (t *gitLogTool) Name() string        { return "git-log" }
func (t *gitLogTool) Description() string { return "Show commit logs." }

func (t *gitLogTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"count": {
				"type": "integer",
				"description": "Number of commits to show (default 10)"
			}
		}
	}`)
}

func (t *gitLogTool) Execute(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	var in gitLogInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	count := in.Count
	if count <= 0 {
		count = 10
	}

	return runGit(ctx, t.workDir, "log", "-n", strconv.Itoa(count))
}

// --- git-status tool ---

type gitStatusTool struct {
	workDir string
}

func (t *gitStatusTool) Name() string { return "git-status" }
func (t *gitStatusTool) Description() string {
	return "Show the working tree status in porcelain format."
}

func (t *gitStatusTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {}
	}`)
}

func (t *gitStatusTool) Execute(ctx context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	return runGit(ctx, t.workDir, "status", "--porcelain")
}

// runGit executes a git command in the given directory and returns the result.
func runGit(ctx context.Context, workDir string, args ...string) (tools.ToolResult, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workDir

	output, err := cmd.CombinedOutput()
	content := strings.TrimRight(string(output), "\n")

	if err != nil {
		return tools.ToolResult{Content: content, IsError: true}, nil
	}
	return tools.ToolResult{Content: content}, nil
}
