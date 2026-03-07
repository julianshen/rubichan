package worktree

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Manager manages git worktrees within a repository.
type Manager struct {
	repoRoot string
	config   Config
}

// NewManager creates a Manager for the given repo root and configuration.
func NewManager(repoRoot string, cfg Config) *Manager {
	return &Manager{
		repoRoot: repoRoot,
		config:   cfg,
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

	// Delete the branch (may already be gone).
	m.git(ctx, m.repoRoot, "branch", "-D", wt.BranchName()) //nolint:errcheck

	return nil
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
