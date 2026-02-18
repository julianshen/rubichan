package builtin

import (
	"context"
	"encoding/json"
	"os/exec"
	"testing"

	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitManifest(t *testing.T) {
	m := GitManifest()

	assert.Equal(t, "git", m.Name)
	assert.Equal(t, "1.0.0", m.Version)
	require.Len(t, m.Types, 1)
	assert.Equal(t, skills.SkillTypeTool, m.Types[0])
	require.Len(t, m.Permissions, 1)
	assert.Equal(t, skills.PermGitRead, m.Permissions[0])
	assert.Empty(t, string(m.Implementation.Backend), "built-in skills should not set a backend")
}

func TestGitRegistersGitTools(t *testing.T) {
	// Create a temporary git repo so the tools can actually run.
	repoDir := initTestGitRepo(t)

	backend := &GitBackend{WorkDir: repoDir}
	m := GitManifest()

	err := backend.Load(m, noopChecker{})
	require.NoError(t, err)

	toolList := backend.Tools()
	require.Len(t, toolList, 3, "git skill should expose exactly 3 tools")

	toolMap := make(map[string]tools.Tool)
	for _, tool := range toolList {
		toolMap[tool.Name()] = tool
	}
	require.Contains(t, toolMap, "git-diff", "git skill must expose a 'git-diff' tool")
	require.Contains(t, toolMap, "git-log", "git skill must expose a 'git-log' tool")
	require.Contains(t, toolMap, "git-status", "git skill must expose a 'git-status' tool")

	// Verify descriptions are non-empty.
	for _, tool := range toolList {
		assert.NotEmpty(t, tool.Description(), "tool %s should have a description", tool.Name())
	}

	// Verify input schemas are valid JSON.
	for _, tool := range toolList {
		var schema map[string]any
		err := json.Unmarshal(tool.InputSchema(), &schema)
		assert.NoError(t, err, "tool %s should have valid JSON input schema", tool.Name())
	}

	ctx := context.Background()

	// Execute git-status (porcelain output on clean repo).
	result, err := toolMap["git-status"].Execute(ctx, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.False(t, result.IsError, "git-status on clean repo should succeed")

	// Execute git-log with default count.
	result, err = toolMap["git-log"].Execute(ctx, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.False(t, result.IsError, "git-log should succeed")
	assert.Contains(t, result.Content, "initial commit")

	// Execute git-log with explicit count.
	result, err = toolMap["git-log"].Execute(ctx, json.RawMessage(`{"count": 1}`))
	require.NoError(t, err)
	assert.False(t, result.IsError, "git-log with count should succeed")

	// Execute git-diff with no range (should show nothing on clean repo).
	result, err = toolMap["git-diff"].Execute(ctx, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.False(t, result.IsError, "git-diff on clean repo should succeed")

	// Execute git-diff with a range.
	result, err = toolMap["git-diff"].Execute(ctx, json.RawMessage(`{"range": "HEAD~1..HEAD"}`))
	require.NoError(t, err)
	assert.False(t, result.IsError, "git-diff with range should succeed")

	// Invalid JSON input for git-diff.
	result, err = toolMap["git-diff"].Execute(ctx, json.RawMessage(`{invalid}`))
	require.NoError(t, err)
	assert.True(t, result.IsError, "git-diff with invalid JSON should return error result")

	// Invalid JSON input for git-log.
	result, err = toolMap["git-log"].Execute(ctx, json.RawMessage(`{invalid}`))
	require.NoError(t, err)
	assert.True(t, result.IsError, "git-log with invalid JSON should return error result")

	// Hooks should be empty.
	assert.Empty(t, backend.Hooks())

	// Unload should succeed.
	assert.NoError(t, backend.Unload())
}

// TestGitToolsInNonGitDir verifies that git tools return errors when run
// outside a git repository.
func TestGitToolsInNonGitDir(t *testing.T) {
	backend := &GitBackend{WorkDir: t.TempDir()}
	err := backend.Load(GitManifest(), noopChecker{})
	require.NoError(t, err)

	ctx := context.Background()
	for _, tool := range backend.Tools() {
		result, err := tool.Execute(ctx, json.RawMessage(`{}`))
		require.NoError(t, err)
		assert.True(t, result.IsError, "tool %s should fail in a non-git directory", tool.Name())
	}
}

// initTestGitRepo creates a temporary git repository with one commit.
func initTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "commit", "--allow-empty", "-m", "initial commit"},
		{"git", "commit", "--allow-empty", "-m", "second commit"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "setup command %v failed: %s", args, string(out))
	}
	return dir
}
