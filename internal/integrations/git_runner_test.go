package integrations

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "cmd %v failed: %s", args, string(out))
	}

	require.NoError(t, os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0o644))
	cmd := exec.Command("git", "add", "hello.txt")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())

	return dir
}

func TestGitRunnerDiff(t *testing.T) {
	dir := setupGitRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello world"), 0o644))

	runner := NewGitRunner(dir)
	diff, err := runner.Diff(context.Background())
	require.NoError(t, err)
	assert.Contains(t, diff, "hello world")
}

func TestGitRunnerLog(t *testing.T) {
	dir := setupGitRepo(t)

	runner := NewGitRunner(dir)
	commits, err := runner.Log(context.Background(), "-1")
	require.NoError(t, err)
	require.Len(t, commits, 1)
	assert.Equal(t, "initial commit", commits[0].Message)
	assert.NotEmpty(t, commits[0].Hash)
	assert.Equal(t, "Test", commits[0].Author)
}

func TestGitRunnerLogWithPipeInMessage(t *testing.T) {
	dir := setupGitRepo(t)

	// Create a commit with pipe characters in the subject
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pipe.txt"), []byte("pipe"), 0o644))
	cmd := exec.Command("git", "add", "pipe.txt")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "fix: handle A|B|C edge case")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())

	runner := NewGitRunner(dir)
	commits, err := runner.Log(context.Background(), "-1")
	require.NoError(t, err)
	require.Len(t, commits, 1)
	assert.Equal(t, "fix: handle A|B|C edge case", commits[0].Message)
}

func TestGitRunnerStatus(t *testing.T) {
	dir := setupGitRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new"), 0o644))

	runner := NewGitRunner(dir)
	statuses, err := runner.Status(context.Background())
	require.NoError(t, err)
	require.Len(t, statuses, 1)
	assert.Equal(t, "new.txt", statuses[0].Path)
	assert.Equal(t, "??", statuses[0].Status)
}

func TestGitRunnerLogEmptyRepo(t *testing.T) {
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		require.NoError(t, cmd.Run())
	}

	runner := NewGitRunner(dir)
	// Empty repo — git log exits with error.
	_, err := runner.Log(context.Background(), "-1")
	require.Error(t, err)
}

func TestGitRunnerRunBadDir(t *testing.T) {
	runner := NewGitRunner("/nonexistent/dir/xyz")
	_, err := runner.Diff(context.Background())
	require.Error(t, err)
}

func TestGitRunnerStatusEmpty(t *testing.T) {
	dir := setupGitRepo(t)
	// No changes — clean working tree.
	runner := NewGitRunner(dir)
	statuses, err := runner.Status(context.Background())
	require.NoError(t, err)
	assert.Empty(t, statuses)
}

func TestGitRunnerStatusRename(t *testing.T) {
	dir := setupGitRepo(t)

	// Rename a file via git mv and stage it.
	cmd := exec.Command("git", "mv", "hello.txt", "renamed.txt")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())

	runner := NewGitRunner(dir)
	statuses, err := runner.Status(context.Background())
	require.NoError(t, err)
	require.Len(t, statuses, 1)
	assert.Equal(t, "renamed.txt", statuses[0].Path)
	assert.Equal(t, "R", statuses[0].Status)
}
