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
