package pipeline

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupGitRepo creates a temp git repo with an initial commit and a second
// commit containing a hello.go file.
func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "commit", "--allow-empty", "-m", "initial"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "cmd %v failed: %s", args, out)
	}

	err := os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package main\n"), 0644)
	require.NoError(t, err)
	for _, args := range [][]string{
		{"git", "add", "hello.go"},
		{"git", "commit", "-m", "add hello"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "cmd %v failed: %s", args, out)
	}

	return dir
}

func TestExtractDiff(t *testing.T) {
	dir := setupGitRepo(t)

	diff, err := ExtractDiff(context.Background(), dir, "HEAD~1..HEAD")
	require.NoError(t, err)
	assert.Contains(t, diff, "hello.go")
	assert.Contains(t, diff, "package main")
}

func TestExtractDiffDefault(t *testing.T) {
	dir := setupGitRepo(t)

	diff, err := ExtractDiff(context.Background(), dir, "")
	require.NoError(t, err)
	assert.Contains(t, diff, "hello.go")
}

func TestExtractDiffInvalidRange(t *testing.T) {
	dir := setupGitRepo(t)

	_, err := ExtractDiff(context.Background(), dir, "nonexistent..alsonotreal")
	require.Error(t, err)
}
