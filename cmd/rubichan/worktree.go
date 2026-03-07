package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/julianshen/rubichan/internal/worktree"
)

func worktreeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worktree",
		Short: "Manage git worktrees",
		Long:  "List, remove, and clean up git worktrees used for isolated agent sessions.",
	}

	cmd.AddCommand(worktreeListCmd())
	cmd.AddCommand(worktreeRemoveCmd())
	cmd.AddCommand(worktreeCleanupCmd())

	return cmd
}

func worktreeListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List active worktrees",
		RunE: func(_ *cobra.Command, _ []string) error {
			mgr, err := newWorktreeManager()
			if err != nil {
				return err
			}

			wts, err := mgr.List(context.Background())
			if err != nil {
				return fmt.Errorf("listing worktrees: %w", err)
			}

			if len(wts) == 0 {
				fmt.Println("No active worktrees.")
				return nil
			}

			for _, wt := range wts {
				status := "clean"
				if wt.HasChanges {
					status = "dirty"
				}
				fmt.Printf("%-20s %-8s %s\n", wt.Name, status, wt.Dir())
			}
			return nil
		},
	}
}

func worktreeRemoveCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a worktree",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			mgr, err := newWorktreeManager()
			if err != nil {
				return err
			}

			if !force {
				hasChanges, err := mgr.HasChanges(context.Background(), name)
				if err != nil {
					return fmt.Errorf("checking worktree: %w", err)
				}
				if hasChanges {
					return fmt.Errorf("worktree %q has uncommitted changes; use --force to remove anyway", name)
				}
			}

			if err := mgr.Remove(context.Background(), name); err != nil {
				return fmt.Errorf("removing worktree: %w", err)
			}
			fmt.Printf("Removed worktree %q\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "remove even if worktree has uncommitted changes")
	return cmd
}

func worktreeCleanupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cleanup",
		Short: "Remove clean worktrees and enforce retention limit",
		RunE: func(_ *cobra.Command, _ []string) error {
			mgr, err := newWorktreeManager()
			if err != nil {
				return err
			}

			if err := mgr.Cleanup(context.Background()); err != nil {
				return fmt.Errorf("cleaning up worktrees: %w", err)
			}
			fmt.Println("Cleanup complete.")
			return nil
		},
	}
}

// newWorktreeManager creates a Manager rooted at the current git repo.
func newWorktreeManager() (*worktree.Manager, error) {
	out, err := runGitCommand("rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("not in a git repository: %w", err)
	}
	root := strings.TrimSpace(out)

	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}

	wtCfg := worktree.Config{
		MaxWorktrees: cfg.Worktree.MaxCount,
		BaseBranch:   cfg.Worktree.BaseBranch,
		AutoCleanup:  cfg.Worktree.AutoCleanup,
	}
	return worktree.NewManager(root, wtCfg), nil
}
