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
				return nil
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
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "Project directory (default: current directory)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing AGENT.md")
	cmd.Flags().BoolVar(&hooksOnly, "hooks-only", false, "Run setup hooks only, skip file generation")

	return cmd
}
