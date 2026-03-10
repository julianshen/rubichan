package gittools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitToolsHappyPath(t *testing.T) {
	repo := initRepo(t)

	statusTool := NewStatusTool(repo)
	logTool := NewLogTool(repo)
	showTool := NewShowTool(repo)

	statusResult, err := statusTool.Execute(context.Background(), json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.False(t, statusResult.IsError)

	logResult, err := logTool.Execute(context.Background(), json.RawMessage(`{"limit":1}`))
	require.NoError(t, err)
	assert.False(t, logResult.IsError)
	assert.Contains(t, logResult.Content, "initial commit")

	showResult, err := showTool.Execute(context.Background(), json.RawMessage(`{"rev":"HEAD"}`))
	require.NoError(t, err)
	assert.False(t, showResult.IsError)
	assert.Contains(t, showResult.Content, "initial commit")
}

func TestGitDiffPathAndBlame(t *testing.T) {
	repo := initRepo(t)
	file := filepath.Join(repo, "hello.txt")
	require.NoError(t, os.WriteFile(file, []byte("line1\nline2\n"), 0o644))
	_, _ = run(t, repo, "add", "hello.txt")
	_, _ = run(t, repo, "commit", "-m", "add hello")
	require.NoError(t, os.WriteFile(file, []byte("line1\nline2 updated\n"), 0o644))

	diffTool := NewDiffTool(repo)
	blameTool := NewBlameTool(repo)

	diffResult, err := diffTool.Execute(context.Background(), json.RawMessage(`{"path":"hello.txt"}`))
	require.NoError(t, err)
	assert.False(t, diffResult.IsError)
	assert.Contains(t, diffResult.Content, "line2 updated")

	blameResult, err := blameTool.Execute(context.Background(), json.RawMessage(`{"path":"hello.txt","line_start":1,"line_end":1}`))
	require.NoError(t, err)
	assert.False(t, blameResult.IsError)
	assert.Contains(t, blameResult.Content, "line1")
}

func TestGitToolOutsideRepo(t *testing.T) {
	tool := NewStatusTool(t.TempDir())
	result, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "not in a git repository")
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "init")
	run(t, dir, "config", "user.email", "test@example.com")
	run(t, dir, "config", "user.name", "Test User")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644))
	run(t, dir, "add", "README.md")
	run(t, dir, "commit", "-m", "initial commit")
	return dir
}

func run(t *testing.T, dir string, args ...string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return out, err
}
