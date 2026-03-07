package worktree

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// HookFunc is called during worktree lifecycle events.
// phase is "worktree.create" or "worktree.remove".
// data contains event-specific fields (name, path, base_branch, repo_root).
// Returns (handled bool, err error). If handled is true, the default git
// operation is skipped.
type HookFunc func(phase string, data map[string]any) (handled bool, err error)

// Manager manages git worktrees within a repository.
type Manager struct {
	repoRoot string
	config   Config
	lock     fileLock
	hookFn   HookFunc
}

// NewManager creates a Manager for the given repo root and configuration.
func NewManager(repoRoot string, cfg Config) *Manager {
	return &Manager{
		repoRoot: repoRoot,
		config:   cfg,
		lock:     fileLock{path: filepath.Join(repoRoot, ".rubichan", "worktrees", ".lock")},
	}
}

// HasChanges reports whether the named worktree has uncommitted changes
// or new commits beyond its base branch.
func (m *Manager) HasChanges(ctx context.Context, name string) (bool, error) {
	wt := Worktree{Name: name, RepoRoot: m.repoRoot}
	dir := wt.Dir()

	// Check for uncommitted changes (staged, unstaged, untracked).
	status, err := m.git(ctx, dir, "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("checking worktree status: %w", err)
	}
	if strings.TrimSpace(status) != "" {
		return true, nil
	}

	// Check for commits beyond the base branch.
	base := m.baseBranch()
	log, err := m.git(ctx, dir, "log", base+"..HEAD", "--oneline")
	if err != nil {
		return false, fmt.Errorf("checking worktree commits: %w", err)
	}
	if strings.TrimSpace(log) != "" {
		return true, nil
	}

	return false, nil
}

// Create creates a new git worktree with a named branch. If the worktree
// already exists, it returns the existing one.
func (m *Manager) Create(ctx context.Context, name string) (*Worktree, error) {
	if err := m.lock.Lock(); err != nil {
		return nil, fmt.Errorf("acquiring lock: %w", err)
	}
	defer m.lock.Unlock()

	return m.create(ctx, name)
}

// Remove removes a worktree and deletes its branch.
func (m *Manager) Remove(ctx context.Context, name string) error {
	if err := m.lock.Lock(); err != nil {
		return fmt.Errorf("acquiring lock: %w", err)
	}
	defer m.lock.Unlock()

	return m.remove(ctx, name)
}

// List returns all managed worktrees with their current status.
func (m *Manager) List(ctx context.Context) ([]Worktree, error) {
	return m.list(ctx)
}

// Cleanup removes clean worktrees and enforces the MaxWorktrees retention limit.
// Acquires the lock once for the entire cleanup operation.
func (m *Manager) Cleanup(ctx context.Context) error {
	if err := m.lock.Lock(); err != nil {
		return fmt.Errorf("acquiring lock: %w", err)
	}
	defer m.lock.Unlock()

	worktrees, err := m.list(ctx)
	if err != nil {
		return err
	}

	// Phase 1: Remove all clean worktrees if AutoCleanup is enabled.
	if m.config.AutoCleanup {
		var remaining []Worktree
		for _, wt := range worktrees {
			if !wt.HasChanges {
				if err := m.remove(ctx, wt.Name); err != nil {
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
		for i := range excess {
			if err := m.remove(ctx, worktrees[i].Name); err != nil {
				return fmt.Errorf("enforcing limit, removing %q: %w", worktrees[i].Name, err)
			}
		}
	}

	return nil
}

// SetHookFunc sets the lifecycle hook function.
func (m *Manager) SetHookFunc(fn HookFunc) {
	m.hookFn = fn
}

// --- internal (unlocked) methods ---

func (m *Manager) create(ctx context.Context, name string) (*Worktree, error) {
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

	// Fire worktree.create hook. If it handles creation, skip git command.
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

	branch := wt.BranchName()
	_, err := m.git(ctx, m.repoRoot, "worktree", "add", "-b", branch, wtDir, base)
	if err != nil {
		return nil, fmt.Errorf("creating worktree %q: %w", name, err)
	}

	return wt, nil
}

func (m *Manager) remove(ctx context.Context, name string) error {
	wt := Worktree{Name: name, RepoRoot: m.repoRoot}
	wtDir := wt.Dir()

	// Verify the worktree exists.
	if _, err := os.Stat(wtDir); os.IsNotExist(err) {
		return fmt.Errorf("worktree %q not found", name)
	}

	// Fire worktree.remove hook. If it handles removal, skip git command.
	if m.hookFn != nil {
		data := map[string]any{
			"name":      name,
			"path":      wtDir,
			"repo_root": m.repoRoot,
		}
		handled, err := m.hookFn("worktree.remove", data)
		if err != nil {
			return fmt.Errorf("worktree.remove hook: %w", err)
		}
		if handled {
			return nil
		}
	}

	// Remove the worktree.
	if _, err := m.git(ctx, m.repoRoot, "worktree", "remove", "--force", wtDir); err != nil {
		return fmt.Errorf("removing worktree %q: %w", name, err)
	}

	// Delete the branch (may already be gone).
	m.git(ctx, m.repoRoot, "branch", "-D", wt.BranchName()) //nolint:errcheck

	return nil
}

func (m *Manager) list(ctx context.Context) ([]Worktree, error) {
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
			continue
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

	sort.Slice(worktrees, func(i, j int) bool {
		return worktrees[i].CreatedAt.Before(worktrees[j].CreatedAt)
	})

	return worktrees, nil
}

// baseBranch returns the configured base branch, defaulting to "main".
func (m *Manager) baseBranch() string {
	if m.config.BaseBranch != "" {
		return m.config.BaseBranch
	}
	return "main"
}

// git runs a git command in the given directory and returns its stdout.
func (m *Manager) git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return string(out), nil
}
