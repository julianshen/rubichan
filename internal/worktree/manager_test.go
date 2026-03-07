package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestCreate(t *testing.T) {
	repo := initTestRepo(t)
	mgr := NewManager(repo, DefaultConfig())

	wt, err := mgr.Create(context.Background(), "feature-x")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if wt.Name != "feature-x" {
		t.Errorf("Name = %q, want %q", wt.Name, "feature-x")
	}

	expectedDir := filepath.Join(repo, ".rubichan", "worktrees", "feature-x")
	if wt.Dir() != expectedDir {
		t.Errorf("Dir() = %q, want %q", wt.Dir(), expectedDir)
	}

	// Verify .git file exists (worktree link, not directory)
	info, err := os.Stat(filepath.Join(expectedDir, ".git"))
	if err != nil {
		t.Fatalf("worktree .git not found: %v", err)
	}
	if info.IsDir() {
		t.Error(".git should be a file (worktree link), not a directory")
	}

	// Verify branch was created
	out, err := exec.Command("git", "-C", repo, "branch", "--list", "worktree-feature-x").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "worktree-feature-x") {
		t.Error("branch worktree-feature-x not found")
	}
}

func TestCreate_AlreadyExists(t *testing.T) {
	repo := initTestRepo(t)
	mgr := NewManager(repo, DefaultConfig())

	_, err := mgr.Create(context.Background(), "dup")
	if err != nil {
		t.Fatal(err)
	}

	// Creating the same name again should reuse (not error)
	wt, err := mgr.Create(context.Background(), "dup")
	if err != nil {
		t.Fatalf("Create duplicate: %v", err)
	}
	if wt.Name != "dup" {
		t.Errorf("Name = %q, want %q", wt.Name, "dup")
	}
}

func TestRemove(t *testing.T) {
	repo := initTestRepo(t)
	mgr := NewManager(repo, DefaultConfig())

	_, err := mgr.Create(context.Background(), "to-remove")
	if err != nil {
		t.Fatal(err)
	}

	err = mgr.Remove(context.Background(), "to-remove")
	if err != nil {
		t.Fatal(err)
	}

	// Verify directory is gone
	wtDir := filepath.Join(repo, ".rubichan", "worktrees", "to-remove")
	if _, err := os.Stat(wtDir); !os.IsNotExist(err) {
		t.Error("worktree directory should be removed")
	}

	// Verify branch is gone
	out, err := exec.Command("git", "-C", repo, "branch", "--list", "worktree-to-remove").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "worktree-to-remove") {
		t.Error("branch worktree-to-remove should be deleted")
	}
}

func TestRemove_NotFound(t *testing.T) {
	repo := initTestRepo(t)
	mgr := NewManager(repo, DefaultConfig())

	err := mgr.Remove(context.Background(), "nonexistent")
	if err == nil {
		t.Error("Remove nonexistent should return error")
	}
}

func TestList_Empty(t *testing.T) {
	repo := initTestRepo(t)
	mgr := NewManager(repo, DefaultConfig())

	list, err := mgr.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("List() returned %d items, want 0", len(list))
	}
}

func TestList_WithWorktrees(t *testing.T) {
	repo := initTestRepo(t)
	mgr := NewManager(repo, DefaultConfig())
	ctx := context.Background()

	mgr.Create(ctx, "alpha")
	mgr.Create(ctx, "beta")

	list, err := mgr.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("List() returned %d items, want 2", len(list))
	}

	names := map[string]bool{}
	for _, wt := range list {
		names[wt.Name] = true
	}
	if !names["alpha"] || !names["beta"] {
		t.Errorf("List() missing expected names, got %v", names)
	}
}

func TestCleanup_RemovesClean(t *testing.T) {
	repo := initTestRepo(t)
	mgr := NewManager(repo, DefaultConfig())
	ctx := context.Background()

	mgr.Create(ctx, "clean-one")
	mgr.Create(ctx, "clean-two")

	err := mgr.Cleanup(ctx)
	if err != nil {
		t.Fatal(err)
	}

	list, err := mgr.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("After cleanup, List() returned %d items, want 0", len(list))
	}
}

func TestCleanup_PreservesDirty(t *testing.T) {
	repo := initTestRepo(t)
	mgr := NewManager(repo, DefaultConfig())
	ctx := context.Background()

	mgr.Create(ctx, "dirty")
	mgr.Create(ctx, "clean")

	// Make dirty worktree actually dirty
	wtDir := filepath.Join(repo, ".rubichan", "worktrees", "dirty")
	os.WriteFile(filepath.Join(wtDir, "change.txt"), []byte("changed"), 0o644)

	err := mgr.Cleanup(ctx)
	if err != nil {
		t.Fatal(err)
	}

	list, err := mgr.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "dirty" {
		names := make([]string, len(list))
		for i, wt := range list {
			names[i] = wt.Name
		}
		t.Errorf("After cleanup, expected only 'dirty' to remain, got %v", names)
	}
}

func TestCreate_FiresHook(t *testing.T) {
	repo := initTestRepo(t)
	mgr := NewManager(repo, DefaultConfig())

	var hookCalled bool
	var hookPhase, hookName string
	mgr.SetHookFunc(func(phase string, data map[string]any) (bool, error) {
		hookCalled = true
		hookPhase = phase
		hookName, _ = data["name"].(string)
		return false, nil // don't override default behavior
	})

	_, err := mgr.Create(context.Background(), "hooked")
	if err != nil {
		t.Fatal(err)
	}
	if !hookCalled {
		t.Error("hook was not called")
	}
	if hookPhase != "worktree.create" {
		t.Errorf("hook phase = %q, want %q", hookPhase, "worktree.create")
	}
	if hookName != "hooked" {
		t.Errorf("hook name = %q, want %q", hookName, "hooked")
	}
}

func TestCreate_HookOverrides(t *testing.T) {
	repo := initTestRepo(t)
	mgr := NewManager(repo, DefaultConfig())

	mgr.SetHookFunc(func(phase string, data map[string]any) (bool, error) {
		// Override: create directory ourselves instead of git worktree add.
		dir, _ := data["path"].(string)
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, ".git"), []byte("custom vcs"), 0o644)
		return true, nil
	})

	wt, err := mgr.Create(context.Background(), "custom-vcs")
	if err != nil {
		t.Fatal(err)
	}

	// Verify the custom hook created the directory.
	if _, err := os.Stat(wt.Dir()); err != nil {
		t.Fatalf("custom hook didn't create directory: %v", err)
	}

	// Verify git branch was NOT created (hook overrode default).
	out, _ := exec.Command("git", "-C", repo, "branch", "--list", "worktree-custom-vcs").CombinedOutput()
	if strings.Contains(string(out), "worktree-custom-vcs") {
		t.Error("branch should not exist when hook overrides creation")
	}
}

func TestRemove_FiresHook(t *testing.T) {
	repo := initTestRepo(t)
	mgr := NewManager(repo, DefaultConfig())

	mgr.Create(context.Background(), "hook-rm")

	var hookCalled bool
	var hookPhase string
	mgr.SetHookFunc(func(phase string, data map[string]any) (bool, error) {
		hookCalled = true
		hookPhase = phase
		return false, nil
	})

	mgr.Remove(context.Background(), "hook-rm")
	if !hookCalled {
		t.Error("remove hook was not called")
	}
	if hookPhase != "worktree.remove" {
		t.Errorf("hook phase = %q, want %q", hookPhase, "worktree.remove")
	}
}

func TestCreate_CustomBaseBranch(t *testing.T) {
	repo := initTestRepo(t)
	cfg := DefaultConfig()
	cfg.BaseBranch = "develop"
	mgr := NewManager(repo, cfg)
	ctx := context.Background()

	// Create the develop branch first.
	addWorktree(t, repo, "tmp-develop")
	tmpDir := filepath.Join(repo, ".rubichan", "worktrees", "tmp-develop")
	cmd := exec.Command("git", "checkout", "-b", "develop")
	cmd.Dir = tmpDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("creating develop branch: %s", string(out))
	}
	// Remove the temp worktree so we can test create with develop as base.
	removeCmd := exec.Command("git", "worktree", "remove", "--force", tmpDir)
	removeCmd.Dir = repo
	removeCmd.CombinedOutput()

	wt, err := mgr.Create(ctx, "from-develop")
	if err != nil {
		t.Fatalf("Create with custom base branch: %v", err)
	}
	if wt.Name != "from-develop" {
		t.Errorf("Name = %q, want %q", wt.Name, "from-develop")
	}

	mgr.Remove(ctx, "from-develop")
}

func TestHasChanges_NonExistent(t *testing.T) {
	repo := initTestRepo(t)
	mgr := NewManager(repo, DefaultConfig())

	_, err := mgr.HasChanges(context.Background(), "does-not-exist")
	if err == nil {
		t.Error("expected error for non-existent worktree")
	}
}

func TestLock_BasicLockUnlock(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")
	l := &fileLock{path: lockPath}

	if err := l.Lock(); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if err := l.Unlock(); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
}

func TestLock_TryLockConflict(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")

	l1 := &fileLock{path: lockPath}
	if err := l1.Lock(); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	defer l1.Unlock()

	l2 := &fileLock{path: lockPath}
	err := l2.TryLock()
	if err == nil {
		l2.Unlock()
		t.Error("expected TryLock to fail when lock is held")
	}
}

func TestLock_UnlockNil(t *testing.T) {
	l := &fileLock{path: "/tmp/unused.lock"}
	if err := l.Unlock(); err != nil {
		t.Errorf("Unlock on nil file should not error: %v", err)
	}
}

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"feature-a", false},
		{"my-worktree", false},
		{"fix_123", false},
		{"", true},
		{"../escape", true},
		{"foo..bar", true},
		{"has space", true},
		{"has/slash", true},
		{"has\\backslash", true},
		{"has:colon", true},
		{"has\ttab", true},
		{"has\nnewline", true},
	}
	for _, tt := range tests {
		err := validateName(tt.name)
		if tt.wantErr && err == nil {
			t.Errorf("validateName(%q) = nil, want error", tt.name)
		}
		if !tt.wantErr && err != nil {
			t.Errorf("validateName(%q) = %v, want nil", tt.name, err)
		}
	}
}

func TestCreate_InvalidName(t *testing.T) {
	repo := initTestRepo(t)
	mgr := NewManager(repo, DefaultConfig())
	_, err := mgr.Create(context.Background(), "../escape")
	if err == nil {
		t.Error("expected error for path traversal name")
	}
}

func TestLock_InvalidPath(t *testing.T) {
	// Lock on path under a file (not a directory) should fail.
	dir := t.TempDir()
	filePath := filepath.Join(dir, "afile")
	os.WriteFile(filePath, []byte("x"), 0o644)

	l := &fileLock{path: filepath.Join(filePath, "sub", "lock")}
	err := l.Lock()
	if err == nil {
		l.Unlock()
		t.Error("expected error when lock parent is a file")
	}
}

func TestCleanup_EnforcesMaxWorktrees(t *testing.T) {
	repo := initTestRepo(t)
	cfg := DefaultConfig()
	cfg.MaxWorktrees = 2
	mgr := NewManager(repo, cfg)
	ctx := context.Background()

	// Create 3 dirty worktrees
	for _, name := range []string{"oldest", "middle", "newest"} {
		mgr.Create(ctx, name)
		wtDir := filepath.Join(repo, ".rubichan", "worktrees", name)
		os.WriteFile(filepath.Join(wtDir, "change.txt"), []byte("dirty"), 0o644)
	}

	err := mgr.Cleanup(ctx)
	if err != nil {
		t.Fatal(err)
	}

	list, err := mgr.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) > 2 {
		t.Errorf("After cleanup with max=2, got %d worktrees", len(list))
	}
}
