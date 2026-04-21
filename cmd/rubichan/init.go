package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/internal/hooks"
	"github.com/julianshen/rubichan/internal/skills"
)

func initCmd() *cobra.Command {
	var (
		dir        string
		force      bool
		hooksOnly  bool
		trustHooks bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a project with AGENT.md and .agent/ structure",
		Long: `Scans the codebase, generates an AGENT.md with project-specific rules,
and creates the .agent/ directory structure (skills/, hooks/).

Uses detected build systems, test frameworks, and linter configs to populate
AGENT.md sections. Sections that cannot be auto-detected use TODO placeholders.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if dir == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("getting working directory: %w", err)
				}
				dir = cwd
			}

			if hooksOnly {
				fmt.Fprintf(cmd.OutOrStdout(), "Running setup hooks (mode=hooks-only)...\n")
				return runSetupHooks(cmd.Context(), cmd.OutOrStdout(), dir, trustHooks)
			}

			for _, sub := range []string{".agent/skills", ".agent/hooks"} {
				if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
					return fmt.Errorf("creating %s: %w", sub, err)
				}
			}

			info := commands.DetectProjectInfo(dir)
			content := commands.GenerateContent("AGENT.md", info)

			target := filepath.Join(dir, "AGENT.md")
			flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
			if !force {
				flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
			}
			f, err := os.OpenFile(target, flags, 0o644)
			if err != nil {
				if errors.Is(err, os.ErrExist) {
					return fmt.Errorf("AGENT.md already exists in %s; use --force to overwrite", dir)
				}
				return fmt.Errorf("writing AGENT.md: %w", err)
			}
			_, writeErr := f.WriteString(content)
			closeErr := f.Close()
			if writeErr != nil {
				return fmt.Errorf("writing AGENT.md: %w", writeErr)
			}
			if closeErr != nil {
				return fmt.Errorf("writing AGENT.md: %w", closeErr)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Generated AGENT.md and .agent/ structure in %s\n", dir)
			return runSetupHooks(cmd.Context(), cmd.OutOrStdout(), dir, trustHooks)
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "Project directory (default: current directory)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing AGENT.md")
	cmd.Flags().BoolVar(&hooksOnly, "hooks-only", false, "Run setup hooks only, skip file generation")
	cmd.Flags().BoolVar(&trustHooks, "trust-hooks", false, "Execute setup hooks from .agent/hooks.toml (runs shell commands; review the file first)")

	return cmd
}

// runSetupHooks loads .agent/hooks.toml in dir and dispatches HookOnSetup to
// any registered handlers. Hooks are shell commands, so they only run when
// trustHooks is true — mirroring the trust gate used by the agent entry points
// so an attacker who can write .agent/hooks.toml cannot get code execution via
// `rubichan init`.
func runSetupHooks(ctx context.Context, out io.Writer, dir string, trustHooks bool) error {
	configs, err := hooks.LoadHooksTOML(dir)
	if err != nil {
		return fmt.Errorf("loading .agent/hooks.toml: %w", err)
	}
	if len(configs) == 0 {
		return nil
	}

	if !trustHooks {
		fmt.Fprintf(out, "Skipping %d setup hook(s) in .agent/hooks.toml — re-run with --trust-hooks after reviewing the file to execute them.\n", len(configs))
		return nil
	}

	lm := skills.NewLifecycleManager()
	hooks.NewUserHookRunner(configs, dir).RegisterIntoLM(lm)

	if _, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnSetup,
		Ctx:   ctx,
		Data:  map[string]any{"mode": "init", "dir": dir},
	}); err != nil {
		return fmt.Errorf("dispatching setup hooks: %w", err)
	}
	return nil
}
