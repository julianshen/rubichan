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
