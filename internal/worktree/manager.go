package worktree

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
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
