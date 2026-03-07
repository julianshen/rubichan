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
