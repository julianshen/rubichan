package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/julianshen/rubichan/internal/commands"
)

func initCmd() *cobra.Command {
	var (
		dir       string
		force     bool
		hooksOnly bool
		// maintenance is reserved for future use (setup hooks with maintenance context).
		maintenance bool
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

			if hooksOnly || maintenance {
				mode := "hooks-only"
				if maintenance {
					mode = "maintenance"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Running setup hooks (mode=%s)...\n", mode)
				return nil
			}

			target := filepath.Join(dir, "AGENT.md")
			if _, err := os.Stat(target); err == nil && !force {
				return fmt.Errorf("AGENT.md already exists in %s; use --force to overwrite", dir)
			} else if err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("checking for existing AGENT.md: %w", err)
			}

			for _, sub := range []string{".agent/skills", ".agent/hooks"} {
				if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
					return fmt.Errorf("creating %s: %w", sub, err)
				}
			}

			info := commands.DetectProjectInfo(dir)
			content := commands.GenerateContent("AGENT.md", info)

			if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
				return fmt.Errorf("writing AGENT.md: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Generated AGENT.md and .agent/ structure in %s\n", dir)
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "Project directory (default: current directory)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing AGENT.md")
	cmd.Flags().BoolVar(&hooksOnly, "hooks-only", false, "Run setup hooks only, skip file generation")
	cmd.Flags().BoolVar(&maintenance, "maintenance", false, "Run setup hooks with maintenance context")

	return cmd
}
