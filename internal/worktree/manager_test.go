package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initTestRepo creates a temporary bare-bones git repo with one initial commit.
// Returns the repo root path. The directory is cleaned up when t finishes.
func initTestRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.name", "Test User"},
		{"git", "config", "user.email", "test@example.com"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %v\n%s", args, err, out)
		}
	}

	// Create an initial commit so HEAD exists.
	initFile := filepath.Join(dir, "README")
	if err := os.WriteFile(initFile, []byte("init"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "add", "README"},
		{"git", "commit", "-m", "initial commit"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %v\n%s", args, err, out)
		}
	}

	return dir
}

// addWorktree creates a real git worktree rooted under .rubichan/worktrees/<name>.
func addWorktree(t *testing.T, repoRoot, name string) string {
	t.Helper()

	branch := "worktree-" + name
	wtDir := filepath.Join(repoRoot, ".rubichan", "worktrees", name)

	cmd := exec.Command("git", "worktree", "add", "-b", branch, wtDir)
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add failed: %v\n%s", err, out)
	}

	return wtDir
}

func TestHasChanges_Clean(t *testing.T) {
	repo := initTestRepo(t)
	addWorktree(t, repo, "clean-wt")

	mgr := NewManager(repo, DefaultConfig())
	changed, err := mgr.HasChanges(context.Background(), "clean-wt")
	if err != nil {
		t.Fatalf("HasChanges returned error: %v", err)
	}
	if changed {
		t.Error("HasChanges = true for a clean worktree, want false")
	}
}

func TestHasChanges_Dirty(t *testing.T) {
	repo := initTestRepo(t)
	wtDir := addWorktree(t, repo, "dirty-wt")

	// Create an untracked file to make the worktree dirty.
	if err := os.WriteFile(filepath.Join(wtDir, "newfile.txt"), []byte("dirty"), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(repo, DefaultConfig())
	changed, err := mgr.HasChanges(context.Background(), "dirty-wt")
	if err != nil {
		t.Fatalf("HasChanges returned error: %v", err)
	}
	if !changed {
		t.Error("HasChanges = false for a dirty worktree, want true")
	}
}

func TestHasChanges_NewCommits(t *testing.T) {
	repo := initTestRepo(t)
	wtDir := addWorktree(t, repo, "commit-wt")

	// Make a commit in the worktree so it's ahead of main.
	newFile := filepath.Join(wtDir, "feature.go")
	if err := os.WriteFile(newFile, []byte("package feature"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "add", "feature.go"},
		{"git", "commit", "-m", "add feature"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = wtDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %v\n%s", args, err, out)
		}
	}

	mgr := NewManager(repo, DefaultConfig())
	changed, err := mgr.HasChanges(context.Background(), "commit-wt")
	if err != nil {
		t.Fatalf("HasChanges returned error: %v", err)
	}
	if !changed {
		t.Error("HasChanges = false for a worktree with new commits, want true")
	}
}
