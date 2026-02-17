package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/store"
)

const defaultRegistryURL = "https://registry.rubichan.dev"

// skillCmd returns the top-level "skill" command with list, info, and search subcommands.
func skillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Manage skills",
		Long:  "List, inspect, and search for skills.",
	}

	cmd.AddCommand(skillListCmd())
	cmd.AddCommand(skillInfoCmd())
	cmd.AddCommand(skillSearchCmd())

	return cmd
}

// resolveStorePath returns the store path from the flag or the default location.
func resolveStorePath(cmd *cobra.Command) (string, error) {
	storePath, _ := cmd.Flags().GetString("store")
	if storePath != "" {
		return storePath, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "rubichan", "skills.db"), nil
}

func skillListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed skills",
		Long:  "Display a table of all installed skills with their name, version, and source.",
		RunE: func(cmd *cobra.Command, args []string) error {
			storePath, err := resolveStorePath(cmd)
			if err != nil {
				return err
			}

			s, err := store.NewStore(storePath)
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}
			defer s.Close()

			states, err := s.ListAllSkillStates()
			if err != nil {
				return fmt.Errorf("listing skills: %w", err)
			}

			if len(states) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No skills installed.")
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tVERSION\tSOURCE\tINSTALLED")
			for _, st := range states {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					st.Name, st.Version, st.Source,
					st.InstalledAt.Format(time.RFC3339),
				)
			}
			return w.Flush()
		},
	}
	cmd.Flags().String("store", "", "path to skills database (default: ~/.config/rubichan/skills.db)")
	return cmd
}

func skillInfoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <name>",
		Short: "Show details for an installed skill",
		Long:  "Display the full manifest details for a named skill.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			storePath, err := resolveStorePath(cmd)
			if err != nil {
				return err
			}

			s, err := store.NewStore(storePath)
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}
			defer s.Close()

			state, err := s.GetSkillState(name)
			if err != nil {
				return fmt.Errorf("looking up skill: %w", err)
			}
			if state == nil {
				return fmt.Errorf("skill %q not found", name)
			}

			// The Source field stores the skill directory path.
			skillDir := state.Source
			manifestPath := filepath.Join(skillDir, "SKILL.yaml")

			data, err := os.ReadFile(manifestPath)
			if err != nil {
				return fmt.Errorf("reading manifest: %w", err)
			}

			manifest, err := skills.ParseManifest(data)
			if err != nil {
				return fmt.Errorf("parsing manifest: %w", err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Name:        %s\n", manifest.Name)
			fmt.Fprintf(out, "Version:     %s\n", manifest.Version)
			fmt.Fprintf(out, "Description: %s\n", manifest.Description)

			types := make([]string, len(manifest.Types))
			for i, t := range manifest.Types {
				types[i] = string(t)
			}
			fmt.Fprintf(out, "Types:       %s\n", strings.Join(types, ", "))

			if manifest.Author != "" {
				fmt.Fprintf(out, "Author:      %s\n", manifest.Author)
			}
			if manifest.License != "" {
				fmt.Fprintf(out, "License:     %s\n", manifest.License)
			}

			if len(manifest.Permissions) > 0 {
				perms := make([]string, len(manifest.Permissions))
				for i, p := range manifest.Permissions {
					perms[i] = string(p)
				}
				fmt.Fprintf(out, "Permissions: %s\n", strings.Join(perms, ", "))
			}

			if manifest.Implementation.Backend != "" {
				fmt.Fprintf(out, "Backend:     %s\n", manifest.Implementation.Backend)
			}
			if manifest.Implementation.Entrypoint != "" {
				fmt.Fprintf(out, "Entrypoint:  %s\n", manifest.Implementation.Entrypoint)
			}

			return nil
		},
	}
	cmd.Flags().String("store", "", "path to skills database (default: ~/.config/rubichan/skills.db)")
	return cmd
}

func skillSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search the skill registry",
		Long:  "Search for skills in the remote registry by keyword.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]

			registryURL, _ := cmd.Flags().GetString("registry")
			if registryURL == "" {
				registryURL = defaultRegistryURL
			}

			client := skills.NewRegistryClient(registryURL, nil, 0)

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			results, err := client.Search(ctx, query)
			if err != nil {
				return fmt.Errorf("searching registry: %w", err)
			}

			if len(results) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No results found.")
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tVERSION\tDESCRIPTION")
			for _, r := range results {
				fmt.Fprintf(w, "%s\t%s\t%s\n", r.Name, r.Version, r.Description)
			}
			return w.Flush()
		},
	}
	cmd.Flags().String("registry", "", "registry URL (default: "+defaultRegistryURL+")")
	return cmd
}
