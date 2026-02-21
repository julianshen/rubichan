package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/store"
)

const defaultRegistryURL = "https://registry.rubichan.dev"

// skillCmd returns the top-level "skill" command with list, info, search,
// install, remove, and add subcommands.
func skillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Manage skills",
		Long:  "List, inspect, search, install, remove, add, create, test, and manage permissions for skills.",
	}

	cmd.AddCommand(skillListCmd())
	cmd.AddCommand(skillInfoCmd())
	cmd.AddCommand(skillSearchCmd())
	cmd.AddCommand(skillInstallCmd())
	cmd.AddCommand(skillRemoveCmd())
	cmd.AddCommand(skillAddCmd())
	cmd.AddCommand(skillCreateCmd())
	cmd.AddCommand(skillTestCmd())
	cmd.AddCommand(skillPermissionsCmd())

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
		Long: `Display a table of all installed skills with their name, version, and source.

Use --available to list skills from the remote registry instead.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			available, _ := cmd.Flags().GetBool("available")
			if available {
				return listAvailableSkills(cmd)
			}
			return listInstalledSkills(cmd)
		},
	}
	cmd.Flags().String("store", "", "path to skills database (default: ~/.config/rubichan/skills.db)")
	cmd.Flags().Bool("available", false, "list skills from the remote registry")
	cmd.Flags().String("registry", "", "registry URL (default: "+defaultRegistryURL+")")
	return cmd
}

// listInstalledSkills displays locally installed skills from the store.
func listInstalledSkills(cmd *cobra.Command) error {
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
}

// listAvailableSkills fetches and displays skills from the remote registry.
func listAvailableSkills(cmd *cobra.Command) error {
	registryURL, _ := cmd.Flags().GetString("registry")
	if registryURL == "" {
		registryURL = defaultRegistryURL
	}

	client := skills.NewRegistryClient(registryURL, nil, 0)

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	results, err := client.Search(ctx, "")
	if err != nil {
		return fmt.Errorf("fetching available skills: %w", err)
	}

	if len(results) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No skills available in the registry.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tVERSION\tDESCRIPTION")
	for _, r := range results {
		fmt.Fprintf(w, "%s\t%s\t%s\n", r.Name, r.Version, r.Description)
	}
	return w.Flush()
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

// resolveSkillsDir returns the skills directory from the flag or the default location.
func resolveSkillsDir(cmd *cobra.Command) (string, error) {
	dir, _ := cmd.Flags().GetString("skills-dir")
	if dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "rubichan", "skills"), nil
}

// isLocalPath returns true if source looks like a local directory path
// (contains a slash or starts with '.').
func isLocalPath(source string) bool {
	return strings.Contains(source, "/") || strings.HasPrefix(source, ".")
}

// parseNameVersion splits "name@version" into (name, version). If no '@' is
// present, version defaults to "latest".
func parseNameVersion(source string) (name, version string) {
	if idx := strings.LastIndex(source, "@"); idx > 0 {
		return source[:idx], source[idx+1:]
	}
	return source, "latest"
}

// validSkillNamePattern matches only safe skill names: alphanumeric, hyphens,
// and underscores. This prevents path traversal (e.g., "../../admin") when
// skill names are interpolated into URL paths or filesystem paths.
var validSkillNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// validateSkillName returns an error if the name contains characters that
// could cause path traversal or URL injection.
func validateSkillName(name string) error {
	const maxSkillNameLength = 128
	if len(name) > maxSkillNameLength {
		return fmt.Errorf("invalid skill name %q: exceeds maximum length of %d characters", name, maxSkillNameLength)
	}
	if !validSkillNamePattern.MatchString(name) {
		return fmt.Errorf("invalid skill name %q: must contain only letters, digits, hyphens, and underscores", name)
	}
	return nil
}

// copyDir recursively copies a directory tree from src to dst. Files are
// copied with their original permissions.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, relPath)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		return copyFile(path, target)
	})
}

// copyFile copies a single file from src to dst, preserving permissions.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return fmt.Errorf("create dest: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	return nil
}

func skillInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install <source>",
		Short: "Install a skill from a local path or registry",
		Long: `Install a skill from a local directory path or the remote registry.

If source contains '/' or starts with '.', it is treated as a local directory.
Otherwise it is treated as a registry skill name. Use name@version to pin a
specific version; otherwise "latest" is used.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := args[0]

			storePath, err := resolveStorePath(cmd)
			if err != nil {
				return err
			}
			skillsDir, err := resolveSkillsDir(cmd)
			if err != nil {
				return err
			}

			if isLocalPath(source) {
				return installFromLocal(cmd, source, skillsDir, storePath)
			}
			return installFromRegistry(cmd, source, skillsDir, storePath)
		},
	}
	cmd.Flags().String("store", "", "path to skills database (default: ~/.config/rubichan/skills.db)")
	cmd.Flags().String("skills-dir", "", "directory to install skills into (default: ~/.config/rubichan/skills/)")
	cmd.Flags().String("registry", "", "registry URL (default: "+defaultRegistryURL+")")
	return cmd
}

// installFromLocal copies a skill from a local directory, validates its
// manifest, and saves install state to the store.
func installFromLocal(cmd *cobra.Command, source, skillsDir, storePath string) error {
	// Validate SKILL.yaml exists in source.
	manifestPath := filepath.Join(source, "SKILL.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("reading SKILL.yaml from %s: %w", source, err)
	}

	manifest, err := skills.ParseManifest(data)
	if err != nil {
		return fmt.Errorf("invalid manifest: %w", err)
	}

	dest := filepath.Join(skillsDir, manifest.Name)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("creating skill directory: %w", err)
	}

	if err := copyDir(source, dest); err != nil {
		return fmt.Errorf("copying skill: %w", err)
	}

	// Save state to store.
	s, err := store.NewStore(storePath)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}
	defer s.Close()

	if err := s.SaveSkillState(store.SkillInstallState{
		Name:    manifest.Name,
		Version: manifest.Version,
		Source:  dest,
	}); err != nil {
		return fmt.Errorf("saving skill state: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Installed skill %q (v%s) from local path\n", manifest.Name, manifest.Version)
	return nil
}

// installFromRegistry downloads a skill from the remote registry, validates
// its manifest, and saves install state. If the version is a SemVer range
// (e.g., "^1.0.0", "~1.2"), it resolves the constraint against available
// versions before downloading.
func installFromRegistry(cmd *cobra.Command, source, skillsDir, storePath string) error {
	name, version := parseNameVersion(source)

	if err := validateSkillName(name); err != nil {
		return err
	}

	registryURL, _ := cmd.Flags().GetString("registry")
	if registryURL == "" {
		registryURL = defaultRegistryURL
	}

	client := skills.NewRegistryClient(registryURL, nil, 0)

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// Resolve SemVer ranges by fetching available versions from the registry.
	if skills.IsSemVerRange(version) {
		available, err := client.ListVersions(ctx, name)
		if err != nil {
			return fmt.Errorf("listing versions: %w", err)
		}
		resolved, err := skills.ResolveVersion(version, available)
		if err != nil {
			return fmt.Errorf("resolving version %q: %w", version, err)
		}
		version = resolved
	}

	dest := filepath.Join(skillsDir, name)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("creating skill directory: %w", err)
	}

	if err := client.Download(ctx, name, version, dest); err != nil {
		return fmt.Errorf("downloading skill: %w", err)
	}

	// Validate and parse the downloaded manifest.
	manifestPath := filepath.Join(dest, "SKILL.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("reading downloaded SKILL.yaml: %w", err)
	}

	manifest, err := skills.ParseManifest(data)
	if err != nil {
		return fmt.Errorf("invalid downloaded manifest: %w", err)
	}

	// Save state to store.
	s, err := store.NewStore(storePath)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}
	defer s.Close()

	if err := s.SaveSkillState(store.SkillInstallState{
		Name:    manifest.Name,
		Version: manifest.Version,
		Source:  dest,
	}); err != nil {
		return fmt.Errorf("saving skill state: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Installed skill %q (v%s) from registry\n", manifest.Name, manifest.Version)
	return nil
}

func skillRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove an installed skill",
		Long:  "Delete the skill directory and remove its entry from the store.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			storePath, err := resolveStorePath(cmd)
			if err != nil {
				return err
			}
			skillsDir, err := resolveSkillsDir(cmd)
			if err != nil {
				return err
			}

			// Verify skill exists in store before deleting anything.
			s, err := store.NewStore(storePath)
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}
			defer s.Close()

			existing, err := s.GetSkillState(name)
			if err != nil {
				return fmt.Errorf("checking skill state: %w", err)
			}
			if existing == nil {
				return fmt.Errorf("skill %q is not installed", name)
			}

			// Remove from store first, then delete directory.
			if err := s.DeleteSkillState(name); err != nil {
				return fmt.Errorf("removing skill from store: %w", err)
			}

			skillDir := filepath.Join(skillsDir, name)
			if err := os.RemoveAll(skillDir); err != nil {
				return fmt.Errorf("removing skill directory: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Removed skill %q\n", name)
			return nil
		},
	}
	cmd.Flags().String("store", "", "path to skills database (default: ~/.config/rubichan/skills.db)")
	cmd.Flags().String("skills-dir", "", "directory where skills are installed (default: ~/.config/rubichan/skills/)")
	return cmd
}

func skillAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <path>",
		Short: "Add a skill to the current project",
		Long:  "Copy a skill from the given path into the project's .agent/skills/<name>/ directory.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := args[0]

			projectDir, _ := cmd.Flags().GetString("project-dir")
			if projectDir == "" {
				var err error
				projectDir, err = os.Getwd()
				if err != nil {
					return fmt.Errorf("cannot determine working directory: %w", err)
				}
			}

			// Validate SKILL.yaml exists in source.
			manifestPath := filepath.Join(source, "SKILL.yaml")
			data, err := os.ReadFile(manifestPath)
			if err != nil {
				return fmt.Errorf("reading SKILL.yaml from %s: %w", source, err)
			}

			manifest, err := skills.ParseManifest(data)
			if err != nil {
				return fmt.Errorf("invalid manifest: %w", err)
			}

			dest := filepath.Join(projectDir, ".agent", "skills", manifest.Name)
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return fmt.Errorf("creating project skill directory: %w", err)
			}

			if err := copyDir(source, dest); err != nil {
				return fmt.Errorf("copying skill to project: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Added skill %q to project\n", manifest.Name)
			return nil
		},
	}
	cmd.Flags().String("project-dir", "", "project root directory (default: current working directory)")
	return cmd
}

// skillCreateTemplate is the template SKILL.yaml written by "skill create".
const skillCreateTemplate = `name: %s
version: 0.1.0
description: "A new skill"
types:
  - tool
implementation:
  backend: starlark
  entrypoint: skill.star
`

// skillStarTemplate is the template skill.star written by "skill create".
const skillStarTemplate = `# %s - Starlark skill entrypoint
#
# This file is the main entrypoint for your skill.
# Use register_tool() to expose tools to the agent.

def hello(args):
    """A simple hello-world tool."""
    name = args.get("name", "world")
    return {"message": "Hello, " + name + "!"}

register_tool(
    name="hello",
    description="A simple hello-world tool",
    handler=hello,
)
`

func skillCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Scaffold a new skill directory",
		Long:  "Create a new skill directory with a template SKILL.yaml and skill.star file.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			parentDir, _ := cmd.Flags().GetString("dir")
			if parentDir == "" {
				var err error
				parentDir, err = os.Getwd()
				if err != nil {
					return fmt.Errorf("cannot determine working directory: %w", err)
				}
			}

			skillDir := filepath.Join(parentDir, name)
			if err := os.MkdirAll(skillDir, 0o755); err != nil {
				return fmt.Errorf("creating skill directory: %w", err)
			}

			// Write SKILL.yaml template.
			manifestContent := fmt.Sprintf(skillCreateTemplate, name)
			if err := os.WriteFile(
				filepath.Join(skillDir, "SKILL.yaml"),
				[]byte(manifestContent), 0o644,
			); err != nil {
				return fmt.Errorf("writing SKILL.yaml: %w", err)
			}

			// Write skill.star template.
			starContent := fmt.Sprintf(skillStarTemplate, name)
			if err := os.WriteFile(
				filepath.Join(skillDir, "skill.star"),
				[]byte(starContent), 0o644,
			); err != nil {
				return fmt.Errorf("writing skill.star: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Created skill %q in %s\n", name, skillDir)
			return nil
		},
	}
	cmd.Flags().String("dir", "", "output parent directory (default: current working directory)")
	return cmd
}

func skillTestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test <path>",
		Short: "Validate a skill manifest",
		Long:  "Read and validate the SKILL.yaml from the given skill directory.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			skillPath := args[0]

			manifestPath := filepath.Join(skillPath, "SKILL.yaml")
			data, err := os.ReadFile(manifestPath)
			if err != nil {
				return fmt.Errorf("reading SKILL.yaml from %s: %w", skillPath, err)
			}

			manifest, err := skills.ParseManifest(data)
			if err != nil {
				return fmt.Errorf("manifest validation failed: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(),
				"Skill '%s' v%s validated successfully\n",
				manifest.Name, manifest.Version,
			)
			return nil
		},
	}
	return cmd
}

func skillPermissionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "permissions <name>",
		Short: "List or revoke permission approvals for a skill",
		Long:  "Display permission approvals for a skill, or revoke all approvals with --revoke.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			revoke, _ := cmd.Flags().GetBool("revoke")

			storePath, err := resolveStorePath(cmd)
			if err != nil {
				return err
			}

			s, err := store.NewStore(storePath)
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}
			defer s.Close()

			if revoke {
				if err := s.Revoke(name); err != nil {
					return fmt.Errorf("revoking permissions: %w", err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "All permissions revoked for skill '%s'\n", name)
				return nil
			}

			approvals, err := s.ListApprovals(name)
			if err != nil {
				return fmt.Errorf("listing approvals: %w", err)
			}

			if len(approvals) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "No permission approvals for skill '%s'\n", name)
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "PERMISSION\tSCOPE\tAPPROVED_AT")
			for _, a := range approvals {
				fmt.Fprintf(w, "%s\t%s\t%s\n",
					a.Permission, a.Scope,
					a.ApprovedAt.Format(time.RFC3339),
				)
			}
			return w.Flush()
		},
	}
	cmd.Flags().String("store", "", "path to skills database (default: ~/.config/rubichan/skills.db)")
	cmd.Flags().Bool("revoke", false, "revoke all permissions for the skill")
	return cmd
}
