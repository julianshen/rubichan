# Git Worktree Support Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add git worktree support for parallel agent isolation, user-facing session isolation, and headless batch mode.

**Architecture:** A standalone `WorktreeManager` in `internal/worktree/` handles creation, listing, cleanup, and lifecycle hooks. The agent core gains a `WithWorkingDir()` option that threads a directory through initialization. CLI, subagent, and headless callers use the manager to create worktrees and pass paths to agents.

**Tech Stack:** Go stdlib (`os/exec` for git commands, `os` for file lock), `spf13/cobra` for CLI, existing `internal/skills` hook system for lifecycle hooks.

---

### Task 1: Worktree Config Types

**Files:**
- Create: `internal/worktree/config.go`
- Test: `internal/worktree/config_test.go`

**Step 1: Write the failing test**

```go
// internal/worktree/config_test.go
package worktree

import "testing"

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MaxWorktrees != 5 {
		t.Errorf("MaxWorktrees = %d, want 5", cfg.MaxWorktrees)
	}
	if cfg.AutoCleanup != true {
		t.Error("AutoCleanup should default to true")
	}
	if cfg.BaseBranch != "" {
		t.Errorf("BaseBranch = %q, want empty", cfg.BaseBranch)
	}
}

func TestWorktreeBranchName(t *testing.T) {
	wt := Worktree{Name: "feature-auth"}
	if got := wt.BranchName(); got != "worktree-feature-auth" {
		t.Errorf("BranchName() = %q, want %q", got, "worktree-feature-auth")
	}
}

func TestWorktreeDir(t *testing.T) {
	wt := Worktree{Name: "feature-auth", RepoRoot: "/repo"}
	if got := wt.Dir(); got != "/repo/.rubichan/worktrees/feature-auth" {
		t.Errorf("Dir() = %q, want %q", got, "/repo/.rubichan/worktrees/feature-auth")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/worktree/... -v -run 'TestDefault|TestWorktree'`
Expected: FAIL — package does not exist

**Step 3: Write minimal implementation**

```go
// internal/worktree/config.go
package worktree

import (
	"path/filepath"
	"time"
)

// Config holds worktree management settings.
type Config struct {
	MaxWorktrees int    // retention limit, oldest idle GC'd when exceeded
	BaseBranch   string // auto-detect from origin/HEAD if empty
	AutoCleanup  bool   // remove worktrees with no changes on session end
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		MaxWorktrees: 5,
		AutoCleanup:  true,
	}
}

// Worktree represents a managed git worktree.
type Worktree struct {
	Name       string
	RepoRoot   string
	CreatedAt  time.Time
	HasChanges bool
}

// BranchName returns the git branch name for this worktree.
func (w Worktree) BranchName() string {
	return "worktree-" + w.Name
}

// Dir returns the filesystem path for this worktree.
func (w Worktree) Dir() string {
	return filepath.Join(w.RepoRoot, ".rubichan", "worktrees", w.Name)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/worktree/... -v -run 'TestDefault|TestWorktree'`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/worktree/config.go internal/worktree/config_test.go
git commit -m "[BEHAVIORAL] Add worktree config types and Worktree struct"
```

---

### Task 2: WorktreeManager — HasChanges

**Files:**
- Create: `internal/worktree/manager.go`
- Create: `internal/worktree/manager_test.go`

**Step 1: Write the failing test**

```go
// internal/worktree/manager_test.go
package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initTestRepo creates a temporary git repo with one commit.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.name", "test")
	run("config", "user.email", "test@test.com")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "initial")
	return dir
}

func TestHasChanges_Clean(t *testing.T) {
	repo := initTestRepo(t)
	m := NewManager(repo, DefaultConfig())

	// Create a worktree manually for testing HasChanges
	wtPath := filepath.Join(repo, ".rubichan", "worktrees", "test-wt")
	cmd := exec.Command("git", "worktree", "add", "-b", "worktree-test-wt", wtPath, "main")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %s\n%s", err, out)
	}

	changed, err := m.HasChanges(context.Background(), "test-wt")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("HasChanges = true for clean worktree, want false")
	}
}

func TestHasChanges_Dirty(t *testing.T) {
	repo := initTestRepo(t)
	m := NewManager(repo, DefaultConfig())

	wtPath := filepath.Join(repo, ".rubichan", "worktrees", "test-wt")
	cmd := exec.Command("git", "worktree", "add", "-b", "worktree-test-wt", wtPath, "main")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %s\n%s", err, out)
	}

	// Create an uncommitted file in the worktree
	if err := os.WriteFile(filepath.Join(wtPath, "dirty.txt"), []byte("dirty"), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, err := m.HasChanges(context.Background(), "test-wt")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("HasChanges = false for dirty worktree, want true")
	}
}

func TestHasChanges_NewCommits(t *testing.T) {
	repo := initTestRepo(t)
	m := NewManager(repo, DefaultConfig())

	wtPath := filepath.Join(repo, ".rubichan", "worktrees", "test-wt")
	cmd := exec.Command("git", "worktree", "add", "-b", "worktree-test-wt", wtPath, "main")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %s\n%s", err, out)
	}

	// Make a commit in the worktree
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = wtPath
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(wtPath, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "new commit")

	changed, err := m.HasChanges(context.Background(), "test-wt")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("HasChanges = false for worktree with new commits, want true")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/worktree/... -v -run TestHasChanges`
Expected: FAIL — `NewManager` undefined

**Step 3: Write minimal implementation**

```go
// internal/worktree/manager.go
package worktree

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Manager handles creation, listing, and cleanup of git worktrees.
type Manager struct {
	repoRoot string
	config   Config
}

// NewManager creates a Manager for the given repository root.
func NewManager(repoRoot string, cfg Config) *Manager {
	return &Manager{repoRoot: repoRoot, config: cfg}
}

// HasChanges reports whether the named worktree has uncommitted changes
// or new commits beyond its base branch.
func (m *Manager) HasChanges(ctx context.Context, name string) (bool, error) {
	wt := Worktree{Name: name, RepoRoot: m.repoRoot}
	wtDir := wt.Dir()

	// Check for uncommitted changes.
	statusOut, err := m.git(ctx, wtDir, "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("checking status: %w", err)
	}
	if strings.TrimSpace(statusOut) != "" {
		return true, nil
	}

	// Check for new commits beyond base.
	baseBranch := m.baseBranch()
	logOut, err := m.git(ctx, wtDir, "log", baseBranch+"..HEAD", "--oneline")
	if err != nil {
		return false, fmt.Errorf("checking commits: %w", err)
	}
	if strings.TrimSpace(logOut) != "" {
		return true, nil
	}

	return false, nil
}

// baseBranch returns the configured base branch or auto-detects it.
func (m *Manager) baseBranch() string {
	if m.config.BaseBranch != "" {
		return m.config.BaseBranch
	}
	return "main"
}

// git runs a git command in the given directory.
func (m *Manager) git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %s: %s", args[0], string(exitErr.Stderr))
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return string(out), nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/worktree/... -v -run TestHasChanges`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/worktree/manager.go internal/worktree/manager_test.go
git commit -m "[BEHAVIORAL] Add WorktreeManager with HasChanges"
```

---

### Task 3: WorktreeManager — Create & Remove

**Files:**
- Modify: `internal/worktree/manager.go`
- Modify: `internal/worktree/manager_test.go`

**Step 1: Write the failing tests**

```go
// Append to internal/worktree/manager_test.go

func TestCreate(t *testing.T) {
	repo := initTestRepo(t)
	m := NewManager(repo, DefaultConfig())

	wt, err := m.Create(context.Background(), "feature-x")
	if err != nil {
		t.Fatal(err)
	}
	if wt.Name != "feature-x" {
		t.Errorf("Name = %q, want %q", wt.Name, "feature-x")
	}
	expectedDir := filepath.Join(repo, ".rubichan", "worktrees", "feature-x")
	if wt.Dir() != expectedDir {
		t.Errorf("Dir() = %q, want %q", wt.Dir(), expectedDir)
	}

	// Verify the directory exists and has a .git file
	info, err := os.Stat(filepath.Join(expectedDir, ".git"))
	if err != nil {
		t.Fatalf("worktree .git not found: %v", err)
	}
	if info.IsDir() {
		t.Error(".git should be a file (worktree link), not a directory")
	}

	// Verify the branch was created
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
	m := NewManager(repo, DefaultConfig())

	_, err := m.Create(context.Background(), "dup")
	if err != nil {
		t.Fatal(err)
	}

	// Creating the same name again should reuse (not error)
	wt, err := m.Create(context.Background(), "dup")
	if err != nil {
		t.Fatalf("Create duplicate: %v", err)
	}
	if wt.Name != "dup" {
		t.Errorf("Name = %q, want %q", wt.Name, "dup")
	}
}

func TestRemove(t *testing.T) {
	repo := initTestRepo(t)
	m := NewManager(repo, DefaultConfig())

	_, err := m.Create(context.Background(), "to-remove")
	if err != nil {
		t.Fatal(err)
	}

	err = m.Remove(context.Background(), "to-remove")
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
	m := NewManager(repo, DefaultConfig())

	err := m.Remove(context.Background(), "nonexistent")
	if err == nil {
		t.Error("Remove nonexistent should return error")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/worktree/... -v -run 'TestCreate|TestRemove'`
Expected: FAIL — `Create` and `Remove` undefined

**Step 3: Write minimal implementation**

Add to `internal/worktree/manager.go`:

```go
// Create creates a new git worktree with a named branch. If the worktree
// already exists, it returns the existing one.
func (m *Manager) Create(ctx context.Context, name string) (*Worktree, error) {
	wt := &Worktree{Name: name, RepoRoot: m.repoRoot, CreatedAt: time.Now()}
	wtDir := wt.Dir()

	// If worktree already exists, return it.
	if _, err := os.Stat(filepath.Join(wtDir, ".git")); err == nil {
		changed, _ := m.HasChanges(ctx, name)
		wt.HasChanges = changed
		return wt, nil
	}

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(wtDir), 0o755); err != nil {
		return nil, fmt.Errorf("creating worktree parent dir: %w", err)
	}

	base := m.baseBranch()
	branch := wt.BranchName()

	_, err := m.git(ctx, m.repoRoot, "worktree", "add", "-b", branch, wtDir, base)
	if err != nil {
		return nil, fmt.Errorf("creating worktree %q: %w", name, err)
	}

	return wt, nil
}

// Remove removes a worktree and deletes its branch.
func (m *Manager) Remove(ctx context.Context, name string) error {
	wt := Worktree{Name: name, RepoRoot: m.repoRoot}
	wtDir := wt.Dir()

	// Verify the worktree exists.
	if _, err := os.Stat(wtDir); os.IsNotExist(err) {
		return fmt.Errorf("worktree %q not found", name)
	}

	// Remove the worktree.
	if _, err := m.git(ctx, m.repoRoot, "worktree", "remove", "--force", wtDir); err != nil {
		return fmt.Errorf("removing worktree %q: %w", name, err)
	}

	// Delete the branch.
	branch := wt.BranchName()
	if _, err := m.git(ctx, m.repoRoot, "branch", "-D", branch); err != nil {
		// Branch may already be gone; ignore error.
	}

	return nil
}
```

Add imports: `"os"`, `"path/filepath"`, `"time"`

**Step 4: Run test to verify it passes**

Run: `go test ./internal/worktree/... -v -run 'TestCreate|TestRemove'`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/worktree/manager.go internal/worktree/manager_test.go
git commit -m "[BEHAVIORAL] Add WorktreeManager Create and Remove"
```

---

### Task 4: WorktreeManager — List & Cleanup

**Files:**
- Modify: `internal/worktree/manager.go`
- Modify: `internal/worktree/manager_test.go`

**Step 1: Write the failing tests**

```go
// Append to internal/worktree/manager_test.go

func TestList_Empty(t *testing.T) {
	repo := initTestRepo(t)
	m := NewManager(repo, DefaultConfig())

	list, err := m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("List() returned %d items, want 0", len(list))
	}
}

func TestList_WithWorktrees(t *testing.T) {
	repo := initTestRepo(t)
	m := NewManager(repo, DefaultConfig())

	m.Create(context.Background(), "alpha")
	m.Create(context.Background(), "beta")

	list, err := m.List(context.Background())
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
		t.Errorf("List() = %v, want alpha and beta", names)
	}
}

func TestCleanup_RemovesClean(t *testing.T) {
	repo := initTestRepo(t)
	m := NewManager(repo, DefaultConfig())

	m.Create(context.Background(), "clean-one")
	m.Create(context.Background(), "clean-two")

	err := m.Cleanup(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Both are clean, both should be removed by cleanup
	list, err := m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("After cleanup, List() returned %d items, want 0", len(list))
	}
}

func TestCleanup_PreservesDirty(t *testing.T) {
	repo := initTestRepo(t)
	m := NewManager(repo, DefaultConfig())

	m.Create(context.Background(), "dirty")
	m.Create(context.Background(), "clean")

	// Make dirty worktree actually dirty
	wtDir := filepath.Join(repo, ".rubichan", "worktrees", "dirty")
	os.WriteFile(filepath.Join(wtDir, "change.txt"), []byte("changed"), 0o644)

	err := m.Cleanup(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	list, err := m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "dirty" {
		t.Errorf("After cleanup, expected only 'dirty' to remain, got %v", list)
	}
}

func TestCleanup_EnforcesMaxWorktrees(t *testing.T) {
	repo := initTestRepo(t)
	cfg := DefaultConfig()
	cfg.MaxWorktrees = 2
	m := NewManager(repo, cfg)

	// Create 3 dirty worktrees (so none get auto-cleaned for being clean)
	for _, name := range []string{"oldest", "middle", "newest"} {
		m.Create(context.Background(), name)
		wtDir := filepath.Join(repo, ".rubichan", "worktrees", name)
		os.WriteFile(filepath.Join(wtDir, "change.txt"), []byte("dirty"), 0o644)
	}

	err := m.Cleanup(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	list, err := m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) > 2 {
		t.Errorf("After cleanup with max=2, got %d worktrees", len(list))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/worktree/... -v -run 'TestList|TestCleanup'`
Expected: FAIL — `List` and `Cleanup` undefined

**Step 3: Write minimal implementation**

Add to `internal/worktree/manager.go`:

```go
// List returns all managed worktrees with their current status.
func (m *Manager) List(ctx context.Context) ([]Worktree, error) {
	wtBase := filepath.Join(m.repoRoot, ".rubichan", "worktrees")
	entries, err := os.ReadDir(wtBase)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading worktree directory: %w", err)
	}

	var worktrees []Worktree
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		gitFile := filepath.Join(wtBase, name, ".git")
		if _, err := os.Stat(gitFile); os.IsNotExist(err) {
			continue // not a worktree
		}

		wt := Worktree{Name: name, RepoRoot: m.repoRoot}
		info, _ := entry.Info()
		if info != nil {
			wt.CreatedAt = info.ModTime()
		}
		changed, _ := m.HasChanges(ctx, name)
		wt.HasChanges = changed
		worktrees = append(worktrees, wt)
	}

	// Sort by creation time (oldest first).
	sort.Slice(worktrees, func(i, j int) bool {
		return worktrees[i].CreatedAt.Before(worktrees[j].CreatedAt)
	})

	return worktrees, nil
}

// Cleanup removes clean worktrees and enforces the MaxWorktrees retention limit.
func (m *Manager) Cleanup(ctx context.Context) error {
	worktrees, err := m.List(ctx)
	if err != nil {
		return err
	}

	// Phase 1: Remove all clean worktrees if AutoCleanup is enabled.
	if m.config.AutoCleanup {
		var remaining []Worktree
		for _, wt := range worktrees {
			if !wt.HasChanges {
				if err := m.Remove(ctx, wt.Name); err != nil {
					return fmt.Errorf("cleaning %q: %w", wt.Name, err)
				}
			} else {
				remaining = append(remaining, wt)
			}
		}
		worktrees = remaining
	}

	// Phase 2: Enforce retention limit by removing oldest idle worktrees.
	if m.config.MaxWorktrees > 0 && len(worktrees) > m.config.MaxWorktrees {
		excess := len(worktrees) - m.config.MaxWorktrees
		for i := 0; i < excess; i++ {
			if err := m.Remove(ctx, worktrees[i].Name); err != nil {
				return fmt.Errorf("enforcing limit, removing %q: %w", worktrees[i].Name, err)
			}
		}
	}

	return nil
}
```

Add import: `"sort"`

**Step 4: Run test to verify it passes**

Run: `go test ./internal/worktree/... -v -run 'TestList|TestCleanup'`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/worktree/manager.go internal/worktree/manager_test.go
git commit -m "[BEHAVIORAL] Add WorktreeManager List and Cleanup"
```

---

### Task 5: WorktreeManager — File Lock for Concurrency

**Files:**
- Modify: `internal/worktree/manager.go`
- Create: `internal/worktree/lock.go`
- Create: `internal/worktree/lock_test.go`

**Step 1: Write the failing test**

```go
// internal/worktree/lock_test.go
package worktree

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileLock_AcquireRelease(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".lock")

	fl := &fileLock{path: lockPath}
	if err := fl.Lock(); err != nil {
		t.Fatalf("Lock() = %v", err)
	}

	// Lock file should exist
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file not created: %v", err)
	}

	if err := fl.Unlock(); err != nil {
		t.Fatalf("Unlock() = %v", err)
	}
}

func TestFileLock_BlocksConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".lock")

	fl1 := &fileLock{path: lockPath}
	if err := fl1.Lock(); err != nil {
		t.Fatal(err)
	}

	// Second lock attempt should fail (non-blocking TryLock)
	fl2 := &fileLock{path: lockPath}
	if err := fl2.TryLock(); err == nil {
		t.Error("TryLock should fail when lock is held")
		fl2.Unlock()
	}

	fl1.Unlock()

	// After release, should be acquirable again
	if err := fl2.Lock(); err != nil {
		t.Fatalf("Lock after release: %v", err)
	}
	fl2.Unlock()
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/worktree/... -v -run TestFileLock`
Expected: FAIL — `fileLock` undefined

**Step 3: Write minimal implementation**

```go
// internal/worktree/lock.go
package worktree

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// fileLock provides file-based locking using flock(2).
type fileLock struct {
	path string
	f    *os.File
}

// Lock acquires the file lock, blocking until available.
func (l *fileLock) Lock() error {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("creating lock directory: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("opening lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return fmt.Errorf("acquiring lock: %w", err)
	}
	l.f = f
	return nil
}

// TryLock attempts to acquire the lock without blocking.
// Returns an error if the lock is already held.
func (l *fileLock) TryLock() error {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("creating lock directory: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("opening lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return fmt.Errorf("lock already held")
	}
	l.f = f
	return nil
}

// Unlock releases the file lock.
func (l *fileLock) Unlock() error {
	if l.f == nil {
		return nil
	}
	if err := syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN); err != nil {
		l.f.Close()
		return fmt.Errorf("releasing lock: %w", err)
	}
	return l.f.Close()
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/worktree/... -v -run TestFileLock`
Expected: PASS

**Step 5: Wire lock into Manager.Create and Manager.Remove**

Add a `lock` field to `Manager`, initialize in `NewManager`, and wrap `Create`/`Remove` with `lock.Lock()`/`defer lock.Unlock()`.

**Step 6: Run all worktree tests**

Run: `go test ./internal/worktree/... -v`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/worktree/lock.go internal/worktree/lock_test.go internal/worktree/manager.go
git commit -m "[BEHAVIORAL] Add file lock for concurrent worktree access"
```

---

### Task 6: Lifecycle Hooks — Add Hook Phases

**Files:**
- Modify: `internal/skills/types.go`
- Modify: `internal/skills/types_test.go` (if exists, or create)

**Step 1: Write the failing test**

```go
// Test that the new hook phases have correct String() output.
func TestHookPhase_WorktreeStrings(t *testing.T) {
	tests := []struct {
		phase HookPhase
		want  string
	}{
		{HookOnWorktreeCreate, "OnWorktreeCreate"},
		{HookOnWorktreeRemove, "OnWorktreeRemove"},
	}
	for _, tt := range tests {
		if got := tt.phase.String(); got != tt.want {
			t.Errorf("%d.String() = %q, want %q", tt.phase, got, tt.want)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/skills/... -v -run TestHookPhase_Worktree`
Expected: FAIL — `HookOnWorktreeCreate` undefined

**Step 3: Write minimal implementation**

In `internal/skills/types.go`, add two new constants after `HookOnSecurityScanComplete`:

```go
// HookOnWorktreeCreate is called before a git worktree is created.
HookOnWorktreeCreate
// HookOnWorktreeRemove is called before a git worktree is removed.
HookOnWorktreeRemove
```

Add cases to the `String()` method:

```go
case HookOnWorktreeCreate:
    return "OnWorktreeCreate"
case HookOnWorktreeRemove:
    return "OnWorktreeRemove"
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/skills/... -v -run TestHookPhase_Worktree`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/skills/types.go internal/skills/types_test.go
git commit -m "[BEHAVIORAL] Add worktree lifecycle hook phases"
```

---

### Task 7: WorktreeManager — Hook Integration

**Files:**
- Modify: `internal/worktree/manager.go`
- Modify: `internal/worktree/manager_test.go`

**Step 1: Write the failing test**

```go
// Append to manager_test.go

func TestCreate_FiresHook(t *testing.T) {
	repo := initTestRepo(t)
	m := NewManager(repo, DefaultConfig())

	var hookCalled bool
	var hookName string
	m.SetHookFunc(func(phase string, data map[string]any) (bool, error) {
		hookCalled = true
		hookName, _ = data["name"].(string)
		return false, nil // false = don't override default behavior
	})

	_, err := m.Create(context.Background(), "hooked")
	if err != nil {
		t.Fatal(err)
	}
	if !hookCalled {
		t.Error("hook was not called")
	}
	if hookName != "hooked" {
		t.Errorf("hook name = %q, want %q", hookName, "hooked")
	}
}

func TestCreate_HookOverrides(t *testing.T) {
	repo := initTestRepo(t)
	m := NewManager(repo, DefaultConfig())

	m.SetHookFunc(func(phase string, data map[string]any) (bool, error) {
		// Override: create directory ourselves instead of git worktree
		dir, _ := data["path"].(string)
		os.MkdirAll(dir, 0o755)
		// Write a fake .git file so the manager recognizes it
		os.WriteFile(filepath.Join(dir, ".git"), []byte("custom vcs"), 0o644)
		return true, nil // true = override default
	})

	wt, err := m.Create(context.Background(), "custom-vcs")
	if err != nil {
		t.Fatal(err)
	}

	// Verify the custom hook created the directory
	if _, err := os.Stat(wt.Dir()); err != nil {
		t.Fatalf("custom hook didn't create directory: %v", err)
	}

	// Verify git branch was NOT created (hook overrode default)
	out, _ := exec.Command("git", "-C", repo, "branch", "--list", "worktree-custom-vcs").CombinedOutput()
	if strings.Contains(string(out), "worktree-custom-vcs") {
		t.Error("branch should not exist when hook overrides creation")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/worktree/... -v -run 'TestCreate_Fires|TestCreate_Hook'`
Expected: FAIL — `SetHookFunc` undefined

**Step 3: Write minimal implementation**

Add to `Manager`:

```go
// HookFunc is called during worktree lifecycle events.
// phase is "worktree.create" or "worktree.remove".
// data contains event-specific fields (name, path, base_branch, repo_root).
// Returns (handled bool, err error). If handled is true, the default git
// operation is skipped.
type HookFunc func(phase string, data map[string]any) (handled bool, err error)

// Add hookFn field to Manager struct
// hookFn HookFunc

// SetHookFunc sets the lifecycle hook function.
func (m *Manager) SetHookFunc(fn HookFunc) {
	m.hookFn = fn
}
```

Modify `Create` to fire hook before git command:

```go
// In Create, before git worktree add:
if m.hookFn != nil {
    data := map[string]any{
        "name":        name,
        "path":        wtDir,
        "base_branch": base,
        "repo_root":   m.repoRoot,
    }
    handled, err := m.hookFn("worktree.create", data)
    if err != nil {
        return nil, fmt.Errorf("worktree.create hook: %w", err)
    }
    if handled {
        return wt, nil
    }
}
```

Similarly modify `Remove` to fire `worktree.remove` hook.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/worktree/... -v -run 'TestCreate_Fires|TestCreate_Hook'`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/worktree/manager.go internal/worktree/manager_test.go
git commit -m "[BEHAVIORAL] Add lifecycle hook support to WorktreeManager"
```

---

### Task 8: Agent Core — WithWorkingDir Option

**Files:**
- Modify: `internal/agent/agent.go`
- Modify: `internal/agent/agent_test.go`

**Step 1: Write the failing test**

```go
// Test that WithWorkingDir sets the working directory.
func TestWithWorkingDir(t *testing.T) {
	cfg := config.DefaultConfig()
	p := &mockProvider{responses: []string{"hello"}}
	reg := tools.NewRegistry()

	a := New(p, reg, nil, cfg, WithWorkingDir("/custom/dir"))
	if a.workingDir != "/custom/dir" {
		t.Errorf("workingDir = %q, want %q", a.workingDir, "/custom/dir")
	}
}

func TestWithWorkingDir_FallbackToGetwd(t *testing.T) {
	cfg := config.DefaultConfig()
	p := &mockProvider{responses: []string{"hello"}}
	reg := tools.NewRegistry()

	a := New(p, reg, nil, cfg)
	if a.workingDir != "" {
		t.Errorf("workingDir = %q, want empty (fallback to os.Getwd)", a.workingDir)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/... -v -run TestWithWorkingDir`
Expected: FAIL — `WithWorkingDir` undefined, `workingDir` not a field

**Step 3: Write minimal implementation**

Add to `internal/agent/agent.go`:

1. Add `workingDir` field to `Agent` struct (after `pipeline` field):
   ```go
   workingDir string // override working directory (empty = os.Getwd)
   ```

2. Add the option function:
   ```go
   // WithWorkingDir overrides the working directory for all directory-scoped
   // operations (file tool, shell tool, skill discovery, session, memories).
   // If empty, os.Getwd() is used as fallback.
   func WithWorkingDir(dir string) AgentOption {
       return func(a *Agent) { a.workingDir = dir }
   }

   // WorkingDir returns the agent's effective working directory.
   func (a *Agent) WorkingDir() string {
       if a.workingDir != "" {
           return a.workingDir
       }
       wd, _ := os.Getwd()
       return wd
   }
   ```

3. Replace `os.Getwd()` in memory loading (line ~264) with `a.WorkingDir()`.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/... -v -run TestWithWorkingDir`
Expected: PASS

**Step 5: Run full agent test suite**

Run: `go test ./internal/agent/... -v -count=1`
Expected: PASS (no regressions)

**Step 6: Commit**

```bash
git add internal/agent/agent.go internal/agent/agent_test.go
git commit -m "[BEHAVIORAL] Add WithWorkingDir agent option"
```

---

### Task 9: Worktree Config in Config System

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Step 1: Write the failing test**

```go
func TestWorktreeConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Worktree.MaxCount != 5 {
		t.Errorf("MaxCount = %d, want 5", cfg.Worktree.MaxCount)
	}
	if cfg.Worktree.AutoCleanup != true {
		t.Error("AutoCleanup should default to true")
	}
}

func TestWorktreeConfigFromTOML(t *testing.T) {
	content := `
[worktree]
max_count = 10
base_branch = "develop"
auto_cleanup = false
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(content), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Worktree.MaxCount != 10 {
		t.Errorf("MaxCount = %d, want 10", cfg.Worktree.MaxCount)
	}
	if cfg.Worktree.BaseBranch != "develop" {
		t.Errorf("BaseBranch = %q, want %q", cfg.Worktree.BaseBranch, "develop")
	}
	if cfg.Worktree.AutoCleanup != false {
		t.Error("AutoCleanup should be false")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run TestWorktreeConfig`
Expected: FAIL — `cfg.Worktree` undefined

**Step 3: Write minimal implementation**

Add to `internal/config/config.go`:

```go
// WorktreeConfig holds settings for git worktree management.
type WorktreeConfig struct {
	MaxCount    int    `toml:"max_count"`
	BaseBranch  string `toml:"base_branch"`
	AutoCleanup bool   `toml:"auto_cleanup"`
}
```

Add `Worktree WorktreeConfig` field to `Config` struct (after `Security`).

Add defaults in `DefaultConfig()`:
```go
Worktree: WorktreeConfig{
    MaxCount:    5,
    AutoCleanup: true,
},
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run TestWorktreeConfig`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "[BEHAVIORAL] Add worktree section to config"
```

---

### Task 10: Subagent Integration — Isolation Field

**Files:**
- Modify: `internal/skills/types.go` (AgentDefinition)
- Modify: `internal/agent/agentdef.go` (AgentDef)
- Modify: `internal/config/config.go` (AgentDefConf)

**Step 1: Write the failing test**

```go
// In agentdef_test.go or a new test file
func TestAgentDef_IsolationField(t *testing.T) {
	def := &AgentDef{
		Name:      "test-agent",
		Isolation: "worktree",
	}
	if def.Isolation != "worktree" {
		t.Errorf("Isolation = %q, want %q", def.Isolation, "worktree")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/... -v -run TestAgentDef_Isolation`
Expected: FAIL — `Isolation` not a field

**Step 3: Write minimal implementation**

Add `Isolation` field to three structs:

1. `AgentDef` in `internal/agent/agentdef.go`:
   ```go
   Isolation string `toml:"isolation" yaml:"isolation"` // "", "worktree"
   ```

2. `AgentDefinition` in `internal/skills/types.go`:
   ```go
   Isolation string
   ```

3. `AgentDefConf` in `internal/config/config.go`:
   ```go
   Isolation string `toml:"isolation"`
   ```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/... -v -run TestAgentDef_Isolation`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/agentdef.go internal/skills/types.go internal/config/config.go
git commit -m "[BEHAVIORAL] Add Isolation field to agent definitions"
```

---

### Task 11: CLI — `--worktree` Flag

**Files:**
- Modify: `cmd/rubichan/main.go`

**Step 1: Add the `--worktree` persistent flag**

In the flag registration section (around line 102-118), add:
```go
rootCmd.PersistentFlags().StringVar(&worktreeFlag, "worktree", "", "Run in an isolated git worktree with the given name")
```

Add `worktreeFlag` variable near other flag vars.

**Step 2: Add worktree initialization helper**

```go
// initWorktree creates or reuses a worktree and returns its path.
// Returns empty string if --worktree was not specified.
func initWorktree(cfg *config.Config) (string, *worktree.Manager, error) {
	if worktreeFlag == "" {
		return "", nil, nil
	}

	repoRoot, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", nil, fmt.Errorf("not in a git repository: %w", err)
	}
	root := strings.TrimSpace(string(repoRoot))

	wtCfg := worktree.Config{
		MaxWorktrees: cfg.Worktree.MaxCount,
		BaseBranch:   cfg.Worktree.BaseBranch,
		AutoCleanup:  cfg.Worktree.AutoCleanup,
	}
	mgr := worktree.NewManager(root, wtCfg)

	wt, err := mgr.Create(context.Background(), worktreeFlag)
	if err != nil {
		return "", nil, fmt.Errorf("creating worktree: %w", err)
	}

	return wt.Dir(), mgr, nil
}
```

**Step 3: Wire into `runInteractive()` and `runHeadless()`**

In both functions, after config loading but before `os.Getwd()`:

```go
wtDir, wtMgr, err := initWorktree(cfg)
if err != nil {
    return err
}
// If worktree is active, use its directory instead of cwd
if wtDir != "" {
    // Override cwd for all subsequent operations
    // Pass wtDir to tools, agent via WithWorkingDir
}
// Defer cleanup
if wtMgr != nil {
    defer func() {
        changed, _ := wtMgr.HasChanges(context.Background(), worktreeFlag)
        if changed {
            fmt.Fprintf(os.Stderr, "Worktree '%s' preserved at %s (has changes)\n", worktreeFlag, wtDir)
        } else if cfg.Worktree.AutoCleanup {
            wtMgr.Remove(context.Background(), worktreeFlag)
        }
    }()
}
```

Replace `cwd, err := os.Getwd()` calls with:
```go
cwd := wtDir
if cwd == "" {
    cwd, err = os.Getwd()
    if err != nil {
        return fmt.Errorf("getting working directory: %w", err)
    }
}
```

**Step 4: Test manually**

Run: `go build ./cmd/rubichan && ./rubichan --worktree test-wt --headless --prompt "What directory am I in?"`
Expected: Agent runs in `.rubichan/worktrees/test-wt/`

**Step 5: Run existing tests**

Run: `go test ./cmd/rubichan/... -v`
Expected: PASS

**Step 6: Commit**

```bash
git add cmd/rubichan/main.go
git commit -m "[BEHAVIORAL] Add --worktree flag to CLI"
```

---

### Task 12: CLI — `worktree` Subcommand Group

**Files:**
- Create: `cmd/rubichan/worktree.go`

**Step 1: Write the subcommand implementation**

```go
// cmd/rubichan/worktree.go
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"

	"github.com/julianshen/rubichan/internal/worktree"
	"github.com/spf13/cobra"
)

func newWorktreeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worktree",
		Short: "Manage git worktrees",
	}

	cmd.AddCommand(
		newWorktreeListCmd(),
		newWorktreeRemoveCmd(),
		newWorktreeCleanupCmd(),
	)

	return cmd
}

func newWorktreeListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all managed worktrees",
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := newWorktreeManager()
			if err != nil {
				return err
			}
			list, err := mgr.List(context.Background())
			if err != nil {
				return err
			}
			if len(list) == 0 {
				fmt.Println("No worktrees found.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tBRANCH\tSTATUS\tPATH")
			for _, wt := range list {
				status := "clean"
				if wt.HasChanges {
					status = "modified"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", wt.Name, wt.BranchName(), status, wt.Dir())
			}
			return w.Flush()
		},
	}
}

func newWorktreeRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a worktree",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := newWorktreeManager()
			if err != nil {
				return err
			}
			return mgr.Remove(context.Background(), args[0])
		},
	}
}

func newWorktreeCleanupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cleanup",
		Short: "Remove clean worktrees and enforce retention limit",
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := newWorktreeManager()
			if err != nil {
				return err
			}
			return mgr.Cleanup(context.Background())
		},
	}
}

func newWorktreeManager() (*worktree.Manager, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return nil, fmt.Errorf("not in a git repository")
	}
	root := strings.TrimSpace(string(out))

	cfg, _ := loadConfig()
	wtCfg := worktree.Config{
		MaxWorktrees: cfg.Worktree.MaxCount,
		BaseBranch:   cfg.Worktree.BaseBranch,
		AutoCleanup:  cfg.Worktree.AutoCleanup,
	}
	return worktree.NewManager(root, wtCfg), nil
}
```

**Step 2: Register in main.go**

Add `rootCmd.AddCommand(newWorktreeCmd())` alongside the other subcommands (around line 128-131).

**Step 3: Test manually**

```bash
go build ./cmd/rubichan
./rubichan worktree list
./rubichan worktree cleanup
```

**Step 4: Run linter and format check**

Run: `golangci-lint run ./... && gofmt -l .`
Expected: clean

**Step 5: Commit**

```bash
git add cmd/rubichan/worktree.go cmd/rubichan/main.go
git commit -m "[BEHAVIORAL] Add worktree subcommand group (list, remove, cleanup)"
```

---

### Task 13: Subagent Spawner — Worktree Isolation

**Files:**
- Modify: `internal/agent/subagent.go`
- Modify: `internal/agent/subagent_test.go`

**Step 1: Write the failing test**

```go
func TestSubagentConfig_WorktreeIsolation(t *testing.T) {
	cfg := SubagentConfig{
		Name:      "isolated-worker",
		Isolation: "worktree",
	}
	if cfg.Isolation != "worktree" {
		t.Errorf("Isolation = %q, want %q", cfg.Isolation, "worktree")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/... -v -run TestSubagentConfig_Worktree`
Expected: FAIL — `Isolation` not a field on `SubagentConfig`

**Step 3: Write minimal implementation**

Add `Isolation` field to `SubagentConfig`:
```go
Isolation  string   // "", "worktree" — if "worktree", spawn in isolated worktree
```

In `DefaultSubagentSpawner`, add a `WorktreeManager` field:
```go
WorktreeManager *worktree.Manager // optional, needed for isolation: "worktree"
```

In `Spawn()`, before creating the child agent, check for worktree isolation:
```go
var wtCleanup func()
var workDir string

if cfg.Isolation == "worktree" && s.WorktreeManager != nil {
    wtName := fmt.Sprintf("subagent-%s-%s", cfg.Name, uuid.NewString()[:8])
    wt, err := s.WorktreeManager.Create(ctx, wtName)
    if err != nil {
        return nil, fmt.Errorf("creating worktree for subagent: %w", err)
    }
    workDir = wt.Dir()
    wtCleanup = func() {
        changed, _ := s.WorktreeManager.HasChanges(ctx, wtName)
        if !changed {
            s.WorktreeManager.Remove(ctx, wtName)
        }
    }
}

// Pass workDir to child agent via WithWorkingDir if set
```

After the spawn loop completes:
```go
if wtCleanup != nil {
    wtCleanup()
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/... -v -run TestSubagentConfig_Worktree`
Expected: PASS

**Step 5: Run full agent test suite**

Run: `go test ./internal/agent/... -v -count=1`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/agent/subagent.go internal/agent/subagent_test.go
git commit -m "[BEHAVIORAL] Add worktree isolation support to subagent spawner"
```

---

### Task 14: Integration Test — Full Worktree Lifecycle

**Files:**
- Create: `internal/worktree/integration_test.go`

**Step 1: Write the integration test**

```go
// internal/worktree/integration_test.go
package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFullLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	repo := initTestRepo(t)
	cfg := DefaultConfig()
	cfg.MaxWorktrees = 3
	m := NewManager(repo, cfg)
	ctx := context.Background()

	// 1. Create a worktree
	wt, err := m.Create(ctx, "feature-a")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt.Dir(), ".git")); err != nil {
		t.Fatal("worktree not created on disk")
	}

	// 2. Verify it appears in list
	list, err := m.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "feature-a" {
		t.Fatalf("List = %v, want [feature-a]", list)
	}

	// 3. Clean worktree has no changes
	changed, err := m.HasChanges(ctx, "feature-a")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("fresh worktree should have no changes")
	}

	// 4. Make changes, verify HasChanges
	os.WriteFile(filepath.Join(wt.Dir(), "work.txt"), []byte("work"), 0o644)
	changed, err = m.HasChanges(ctx, "feature-a")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("modified worktree should have changes")
	}

	// 5. Create more worktrees to test retention
	m.Create(ctx, "feature-b")
	m.Create(ctx, "feature-c")
	m.Create(ctx, "feature-d")

	list, _ = m.List(ctx)
	if len(list) != 4 {
		t.Fatalf("expected 4 worktrees, got %d", len(list))
	}

	// 6. Cleanup: should remove clean ones (b, c, d) and keep dirty (a)
	// Then enforce max=3 — only feature-a remains (which is under limit)
	err = m.Cleanup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	list, _ = m.List(ctx)
	if len(list) != 1 || list[0].Name != "feature-a" {
		t.Fatalf("after cleanup, expected [feature-a], got %v", list)
	}

	// 7. Remove explicitly
	err = m.Remove(ctx, "feature-a")
	if err != nil {
		t.Fatal(err)
	}
	list, _ = m.List(ctx)
	if len(list) != 0 {
		t.Fatalf("after remove, expected empty, got %v", list)
	}

	// 8. Verify branch is gone
	out, _ := exec.Command("git", "-C", repo, "branch", "--list", "worktree-feature-a").CombinedOutput()
	if strings.Contains(string(out), "worktree-feature-a") {
		t.Error("branch should be deleted after remove")
	}
}
```

**Step 2: Run the integration test**

Run: `go test ./internal/worktree/... -v -run TestFullLifecycle`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/worktree/integration_test.go
git commit -m "[BEHAVIORAL] Add worktree integration test"
```

---

### Task 15: Lint, Format, Coverage Check

**Step 1: Run linter**

Run: `golangci-lint run ./...`
Expected: No errors

**Step 2: Run formatter**

Run: `gofmt -l .`
Expected: No files listed

**Step 3: Check coverage**

Run: `go test -cover ./internal/worktree/...`
Expected: >90% coverage

**Step 4: Run full test suite**

Run: `go test ./... -count=1`
Expected: All pass

**Step 5: Commit any fixes**

```bash
git commit -m "[STRUCTURAL] Fix lint and formatting issues"
```

---

## Task Summary

| Task | Component | Description |
|------|-----------|-------------|
| 1 | `internal/worktree/config.go` | Config types, Worktree struct |
| 2 | `internal/worktree/manager.go` | HasChanges method |
| 3 | `internal/worktree/manager.go` | Create & Remove methods |
| 4 | `internal/worktree/manager.go` | List & Cleanup methods |
| 5 | `internal/worktree/lock.go` | File lock for concurrency |
| 6 | `internal/skills/types.go` | Hook phases for worktree lifecycle |
| 7 | `internal/worktree/manager.go` | Hook integration in Create/Remove |
| 8 | `internal/agent/agent.go` | WithWorkingDir agent option |
| 9 | `internal/config/config.go` | Worktree config section |
| 10 | `internal/agent/agentdef.go` | Isolation field on agent definitions |
| 11 | `cmd/rubichan/main.go` | `--worktree` CLI flag |
| 12 | `cmd/rubichan/worktree.go` | `worktree list/remove/cleanup` subcommands |
| 13 | `internal/agent/subagent.go` | Worktree isolation in subagent spawner |
| 14 | `internal/worktree/integration_test.go` | Full lifecycle integration test |
| 15 | All | Lint, format, coverage check |
