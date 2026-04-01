package worktree_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/worktree"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initTestRepo creates a temporary git repo with an initial commit.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "config", "commit.gpgsign", "false")

	// Create initial commit so branches work.
	f := filepath.Join(dir, "README.md")
	require.NoError(t, os.WriteFile(f, []byte("# Test\n"), 0o644))
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")

	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, string(out))
}

func TestIntegration_FullLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repoRoot := initTestRepo(t)
	ctx := context.Background()

	cfg := worktree.Config{
		MaxWorktrees: 5,
		AutoCleanup:  true,
	}
	mgr := worktree.NewManager(repoRoot, cfg)

	// 1. Create a worktree.
	wt, err := mgr.Create(ctx, "feature-a")
	require.NoError(t, err)
	assert.Equal(t, "feature-a", wt.Name)
	assert.DirExists(t, wt.Dir())

	// 2. List shows the worktree.
	list, err := mgr.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "feature-a", list[0].Name)
	assert.False(t, list[0].HasChanges, "new worktree should be clean")

	// 3. Modify a file in the worktree — makes it dirty.
	testFile := filepath.Join(wt.Dir(), "new-file.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("hello"), 0o644))

	hasChanges, err := mgr.HasChanges(ctx, "feature-a")
	require.NoError(t, err)
	assert.True(t, hasChanges, "worktree should be dirty after adding a file")

	// 4. Cleanup should preserve the dirty worktree.
	require.NoError(t, mgr.Cleanup(ctx))
	list, err = mgr.List(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 1, "dirty worktree should survive cleanup")

	// 5. Remove explicitly should work.
	require.NoError(t, mgr.Remove(ctx, "feature-a"))
	list, err = mgr.List(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 0, "worktree should be gone after explicit remove")
	assert.NoDirExists(t, wt.Dir())
}

func TestIntegration_MultipleWorktrees(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repoRoot := initTestRepo(t)
	ctx := context.Background()

	cfg := worktree.Config{MaxWorktrees: 3, AutoCleanup: true}
	mgr := worktree.NewManager(repoRoot, cfg)

	// Create 3 worktrees.
	for _, name := range []string{"wt-1", "wt-2", "wt-3"} {
		_, err := mgr.Create(ctx, name)
		require.NoError(t, err)
	}

	list, err := mgr.List(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 3)

	// Cleanup should remove all clean worktrees down to within limit.
	require.NoError(t, mgr.Cleanup(ctx))
	list, err = mgr.List(ctx)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(list), 3)
}

func TestIntegration_HookFires(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repoRoot := initTestRepo(t)
	ctx := context.Background()

	cfg := worktree.Config{MaxWorktrees: 5}
	mgr := worktree.NewManager(repoRoot, cfg)

	var hookCalls []string
	mgr.SetHookFunc(func(phase string, data map[string]any) (bool, error) {
		hookCalls = append(hookCalls, phase)
		return false, nil // Don't override default behavior
	})

	_, err := mgr.Create(ctx, "hooked")
	require.NoError(t, err)
	require.NoError(t, mgr.Remove(ctx, "hooked"))

	assert.Contains(t, hookCalls, "worktree.create")
	assert.Contains(t, hookCalls, "worktree.remove")
}
