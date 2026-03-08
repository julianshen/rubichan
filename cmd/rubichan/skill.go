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

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/store"
)

const defaultRegistryURL = "https://registry.rubichan.dev"

const skillManifestNotFoundMsg = "reading skill manifest from %s: open %s: no such file or directory"

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
	cmd.AddCommand(skillAddDirCmd())
	cmd.AddCommand(skillWhyCmd())
	cmd.AddCommand(skillTraceCmd())
	cmd.AddCommand(skillRemoveCmd())
	cmd.AddCommand(skillAddCmd())
	cmd.AddCommand(skillCreateCmd())
	cmd.AddCommand(skillTestCmd())
	cmd.AddCommand(skillLintCmd())
	cmd.AddCommand(skillDevCmd())
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

func resolveConfigFilePath(cmd *cobra.Command) (string, error) {
	cfgPath, _ := cmd.Flags().GetString("config")
	if cfgPath != "" {
		return cfgPath, nil
	}
	if configPath != "" {
		return configPath, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "rubichan", "config.toml"), nil
}

func skillListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed skills",
		Long: `Display a table of all installed skills with their name, version, and source.

Use --available to list skills from the remote registry instead.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			available, err := cmd.Flags().GetBool("available")
			if err != nil {
				return fmt.Errorf("reading --available flag: %w", err)
			}
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
	registryURL, err := cmd.Flags().GetString("registry")
	if err != nil {
		return fmt.Errorf("reading --registry flag: %w", err)
	}
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
			manifest, _, _, err := loadSkillManifest(state.Source)
			if err != nil {
				return err
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
			fmt.Fprintf(out, "InstalledFrom: %s\n", state.Source)

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

func skillWhyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "why <name>",
		Short: "Explain why a skill would activate",
		Long:  "Evaluate the current project context and explain why a named skill would or would not activate.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfgPath, err := resolveConfigFilePath(cmd)
			if err != nil {
				return err
			}
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}

			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("cannot determine home directory: %w", err)
			}
			configDir := filepath.Join(home, ".config", "rubichan")
			userDir := filepath.Join(configDir, "skills")
			if cfg.Skills.UserDir != "" {
				userDir = cfg.Skills.UserDir
			}

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}
			projectDir := filepath.Join(cwd, ".rubichan", "skills")

			loader := skills.NewLoader(userDir, projectDir)
			loader.AddSkillDirs(cfg.Skills.Dirs)
			loader.AddMCPServers(cfg.MCP.Servers)
			if err := registerBuiltinSkillPrompts(loader, configDir); err != nil {
				return err
			}

			discovered, warnings, err := loader.Discover(parseSkillsFlag(skillsFlag))
			if err != nil {
				return err
			}
			for _, warning := range warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", warning)
			}

			report, found := explainSkillActivation(cmd, discovered, cfg, cwd, name)
			if !found {
				return fmt.Errorf("skill %q not found in any discovered source", name)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Name:       %s\n", report.Skill.Manifest.Name)
			fmt.Fprintf(out, "Source:     %s\n", report.Skill.Source)
			fmt.Fprintf(out, "Dir:        %s\n", report.Skill.Dir)
			fmt.Fprintf(out, "Activated:  %t\n", report.Activated)
			fmt.Fprintf(out, "Threshold:  %d\n", activationThreshold(cfg))
			fmt.Fprintf(out, "Score:      %d\n", report.Score.Total)
			fmt.Fprintf(out, "Breakdown:  explicit=%d current_path=%d files=%d keywords=%d languages=%d modes=%d\n",
				report.Score.Explicit,
				report.Score.CurrentPath,
				report.Score.Files,
				report.Score.Keywords,
				report.Score.Languages,
				report.Score.Modes,
			)
			if len(report.MatchedFiles) > 0 {
				fmt.Fprintf(out, "Files:      %s\n", strings.Join(report.MatchedFiles, ", "))
			}
			if len(report.MatchedKeywords) > 0 {
				fmt.Fprintf(out, "Keywords:   %s\n", strings.Join(report.MatchedKeywords, ", "))
			}
			if len(report.MatchedLanguages) > 0 {
				fmt.Fprintf(out, "Languages:  %s\n", strings.Join(report.MatchedLanguages, ", "))
			}
			if len(report.MatchedModes) > 0 {
				fmt.Fprintf(out, "Modes:      %s\n", strings.Join(report.MatchedModes, ", "))
			}

			return nil
		},
	}
	cmd.Flags().String("config", "", "path to config file (default: ~/.config/rubichan/config.toml)")
	cmd.Flags().String("message", "", "message text to evaluate keyword triggers against")
	cmd.Flags().String("mode", "interactive", "execution mode to evaluate")
	cmd.Flags().String("current-path", "", "current focused file path for higher-weight file trigger scoring")
	return cmd
}

func skillTraceCmd() *cobra.Command {
	defaultBudget := skills.DefaultContextBudget()
	cmd := &cobra.Command{
		Use:   "trace",
		Short: "Trace skill activation and prompt budgeting",
		Long:  "Discover skills for the current project, score activation, and show prompt-budget inclusion, truncation, or exclusion decisions.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, cwd, discovered, warnings, err := discoverSkillsForCLI(cmd)
			if err != nil {
				return err
			}
			for _, warning := range warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", warning)
			}

			traceCtx, err := buildTriggerContext(cmd, cwd)
			if err != nil {
				return err
			}
			reports := skills.EvaluateTriggerReports(discovered, traceCtx, activationThreshold(cfg))

			maxTotalTokens, _ := cmd.Flags().GetInt("max-total-tokens")
			maxPerSkillTokens, _ := cmd.Flags().GetInt("max-per-skill-tokens")
			budget := &skills.ContextBudget{
				MaxTotalTokens:    maxTotalTokens,
				MaxPerSkillTokens: maxPerSkillTokens,
			}
			promptReport := buildPromptBudgetTrace(reports, budget)

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Threshold: %d\n", activationThreshold(cfg))
			fmt.Fprintf(out, "Budget:    total=%d per_skill=%d\n", budget.MaxTotalTokens, budget.MaxPerSkillTokens)
			fmt.Fprintf(out, "Discovered: %d\n", len(discovered))

			if len(reports) == 0 {
				fmt.Fprintln(out, "\nNo skills discovered.")
				return nil
			}

			fmt.Fprintln(out, "\nActivation")
			for _, report := range reports {
				name := ""
				if report.Skill.Manifest != nil {
					name = report.Skill.Manifest.Name
				}
				status := "skipped"
				if report.Activated {
					status = "activated"
				}
				fmt.Fprintf(out, "- %s [%s] source=%s score=%d breakdown(explicit=%d current_path=%d files=%d keywords=%d languages=%d modes=%d)\n",
					name,
					status,
					report.Skill.Source,
					report.Score.Total,
					report.Score.Explicit,
					report.Score.CurrentPath,
					report.Score.Files,
					report.Score.Keywords,
					report.Score.Languages,
					report.Score.Modes,
				)
			}

			fmt.Fprintln(out, "\nPrompt Budget")
			if len(promptReport) == 0 {
				fmt.Fprintln(out, "No activated prompt fragments.")
				return nil
			}
			for _, fragment := range promptReport {
				fmt.Fprintf(out, "- %s decision=%s score=%d tokens=%d/%d source=%s\n",
					fragment.SkillName,
					fragment.BudgetDecision,
					fragment.ActivationScore,
					fragment.UsedTokens,
					fragment.OriginalTokens,
					fragment.Source,
				)
			}

			return nil
		},
	}
	cmd.Flags().String("config", "", "path to config file (default: ~/.config/rubichan/config.toml)")
	cmd.Flags().String("message", "", "message text to evaluate keyword triggers against")
	cmd.Flags().String("mode", "interactive", "execution mode to evaluate")
	cmd.Flags().String("current-path", "", "current focused file path for higher-weight file trigger scoring")
	cmd.Flags().Int("max-total-tokens", defaultBudget.MaxTotalTokens, "prompt-budget total token cap")
	cmd.Flags().Int("max-per-skill-tokens", defaultBudget.MaxPerSkillTokens, "prompt-budget per-skill token cap")
	return cmd
}

func activationThreshold(cfg *config.Config) int {
	if cfg == nil || cfg.Skills.ActivationThreshold <= 0 {
		return 1
	}
	return cfg.Skills.ActivationThreshold
}

func explainSkillActivation(cmd *cobra.Command, discovered []skills.DiscoveredSkill, cfg *config.Config, cwd, name string) (skills.ActivationReport, bool) {
	ctx, err := buildTriggerContext(cmd, cwd)
	if err != nil {
		return skills.ActivationReport{}, false
	}
	reports := skills.EvaluateTriggerReports(discovered, ctx, activationThreshold(cfg))
	for _, report := range reports {
		if report.Skill.Manifest != nil && report.Skill.Manifest.Name == name {
			return report, true
		}
	}
	return skills.ActivationReport{}, false
}

func discoverSkillsForCLI(cmd *cobra.Command) (*config.Config, string, []skills.DiscoveredSkill, []string, error) {
	cfgPath, err := resolveConfigFilePath(cmd)
	if err != nil {
		return nil, "", nil, nil, err
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, "", nil, nil, err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, "", nil, nil, fmt.Errorf("cannot determine home directory: %w", err)
	}
	configDir := filepath.Join(home, ".config", "rubichan")
	userDir := filepath.Join(configDir, "skills")
	if cfg.Skills.UserDir != "" {
		userDir = cfg.Skills.UserDir
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, "", nil, nil, fmt.Errorf("getting working directory: %w", err)
	}
	projectDir := filepath.Join(cwd, ".rubichan", "skills")

	loader := skills.NewLoader(userDir, projectDir)
	loader.AddSkillDirs(cfg.Skills.Dirs)
	loader.AddMCPServers(cfg.MCP.Servers)
	if err := registerBuiltinSkillPrompts(loader, configDir); err != nil {
		return nil, "", nil, nil, err
	}

	discovered, warnings, err := loader.Discover(parseSkillsFlag(skillsFlag))
	if err != nil {
		return nil, "", nil, nil, err
	}
	return cfg, cwd, discovered, warnings, nil
}

func buildTriggerContext(cmd *cobra.Command, cwd string) (skills.TriggerContext, error) {
	entries, err := os.ReadDir(cwd)
	if err != nil {
		return skills.TriggerContext{}, fmt.Errorf("reading project directory: %w", err)
	}
	projectFiles := make([]string, 0, len(entries))
	for _, e := range entries {
		projectFiles = append(projectFiles, e.Name())
	}
	message, _ := cmd.Flags().GetString("message")
	mode, _ := cmd.Flags().GetString("mode")
	currentPath, _ := cmd.Flags().GetString("current-path")

	return skills.TriggerContext{
		ProjectFiles:    projectFiles,
		CurrentPath:     currentPath,
		DetectedLangs:   detectLanguages(projectFiles),
		LastUserMessage: message,
		Mode:            mode,
		ExplicitSkills:  parseSkillsFlag(skillsFlag),
	}, nil
}

func buildPromptBudgetTrace(reports []skills.ActivationReport, budget *skills.ContextBudget) []skills.PromptFragment {
	collector := skills.NewPromptCollector()
	for _, report := range reports {
		if !report.Activated || report.Skill.Manifest == nil {
			continue
		}
		fragment, ok := promptFragmentForTrace(report.Skill)
		if !ok {
			continue
		}
		fragment.ActivationScore = report.Score.Total
		collector.Add(fragment)
	}
	return collector.BudgetReport(budget)
}

func promptFragmentForTrace(skill skills.DiscoveredSkill) (skills.PromptFragment, bool) {
	if skill.Manifest == nil || !containsPromptType(skill.Manifest.Types) {
		return skills.PromptFragment{}, false
	}

	fragment := skills.PromptFragment{
		SkillName:        skill.Manifest.Name,
		SystemPromptFile: skill.Manifest.Prompt.SystemPromptFile,
		ContextFiles:     skill.Manifest.Prompt.ContextFiles,
		MaxContextTokens: skill.Manifest.Prompt.MaxContextTokens,
		Source:           skill.Source,
	}

	if skill.InstructionBody != "" {
		fragment.ResolvedPrompt = skill.InstructionBody
		return fragment, true
	}

	if skill.Manifest.Prompt.SystemPromptFile == "" {
		return skills.PromptFragment{}, false
	}

	if skill.Dir == "" {
		fragment.ResolvedPrompt = skill.Manifest.Prompt.SystemPromptFile
		return fragment, true
	}

	promptPath := filepath.Join(skill.Dir, skill.Manifest.Prompt.SystemPromptFile)
	data, err := os.ReadFile(promptPath)
	if err != nil {
		fragment.ResolvedPrompt = fmt.Sprintf("[error reading prompt file %q: %s]", promptPath, err)
		return fragment, true
	}
	fragment.ResolvedPrompt = string(data)
	return fragment, true
}

func containsPromptType(types []skills.SkillType) bool {
	for _, st := range types {
		if st == skills.SkillTypePrompt {
			return true
		}
	}
	return false
}

func detectLanguages(files []string) []string {
	seen := map[string]bool{}
	var langs []string
	for _, file := range files {
		base := filepath.Base(file)
		var lang string
		switch {
		case base == "Dockerfile" || strings.HasPrefix(base, "Dockerfile."):
			lang = "dockerfile"
		case base == "Package.swift":
			lang = "swift"
		case base == "Cargo.toml":
			lang = "rust"
		default:
			switch filepath.Ext(base) {
			case ".go":
				lang = "go"
			case ".rs":
				lang = "rust"
			case ".py":
				lang = "python"
			case ".js":
				lang = "javascript"
			case ".ts":
				lang = "typescript"
			case ".tsx":
				lang = "typescript"
			case ".jsx":
				lang = "javascript"
			case ".swift":
				lang = "swift"
			}
		}
		if lang != "" && !seen[lang] {
			seen[lang] = true
			langs = append(langs, lang)
		}
	}
	return langs
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

func loadSkillManifest(skillDir string) (*skills.SkillManifest, string, string, error) {
	yamlPath := filepath.Join(skillDir, "SKILL.yaml")
	if data, err := os.ReadFile(yamlPath); err == nil {
		manifest, parseErr := skills.ParseManifest(data)
		if parseErr != nil {
			return nil, "", "", fmt.Errorf("invalid manifest: %w", parseErr)
		}
		return manifest, yamlPath, "yaml", nil
	} else if !os.IsNotExist(err) {
		return nil, "", "", fmt.Errorf("reading skill manifest from %s: %w", skillDir, err)
	}

	mdPath := filepath.Join(skillDir, "SKILL.md")
	data, err := os.ReadFile(mdPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", "", fmt.Errorf(skillManifestNotFoundMsg, skillDir, yamlPath)
		}
		return nil, "", "", fmt.Errorf("reading skill manifest from %s: %w", skillDir, err)
	}

	manifest, _, parseErr := skills.ParseInstructionSkill(data)
	if parseErr != nil {
		return nil, "", "", fmt.Errorf("invalid manifest: %w", parseErr)
	}
	return manifest, mdPath, "instruction", nil
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
	manifest, _, _, err := loadSkillManifest(source)
	if err != nil {
		return err
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

func skillAddDirCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add-dir <path>",
		Short: "Register an external skill directory",
		Long:  "Persist an external skill-pack directory in config so Rubichan discovers skills there recursively.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]
			absDir, err := filepath.Abs(dir)
			if err != nil {
				return fmt.Errorf("resolving path: %w", err)
			}
			info, err := os.Stat(absDir)
			if err != nil {
				return fmt.Errorf("stat skill directory: %w", err)
			}
			if !info.IsDir() {
				return fmt.Errorf("skill directory %q is not a directory", absDir)
			}

			cfgPath, err := resolveConfigFilePath(cmd)
			if err != nil {
				return err
			}
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}

			for _, existing := range cfg.Skills.Dirs {
				if existing == absDir {
					fmt.Fprintf(cmd.OutOrStdout(), "Skill directory %q is already registered\n", absDir)
					return nil
				}
			}
			cfg.Skills.Dirs = append(cfg.Skills.Dirs, absDir)
			if err := config.Save(cfgPath, cfg); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Registered skill directory %q\n", absDir)
			return nil
		},
	}
	cmd.Flags().String("config", "", "path to config file (default: ~/.config/rubichan/config.toml)")
	return cmd
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

	registryURL, err := cmd.Flags().GetString("registry")
	if err != nil {
		return fmt.Errorf("reading --registry flag: %w", err)
	}
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
	manifest, _, _, err := loadSkillManifest(dest)
	if err != nil {
		return fmt.Errorf("invalid downloaded manifest: %w", err)
	}

	// Validate that the manifest matches what was requested.
	if manifest.Name != name {
		return fmt.Errorf("downloaded skill declares name %q but %q was requested", manifest.Name, name)
	}
	if version != "" && version != "latest" {
		if manifest.Version != version {
			return fmt.Errorf("downloaded skill declares version %q but %q was requested", manifest.Version, version)
		}
	} else if manifest.Version == "" {
		return fmt.Errorf("downloaded skill declares empty version")
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
		Long:  "Copy a skill from the given path into the project's .rubichan/skills/<name>/ directory.",
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

			manifest, _, _, err := loadSkillManifest(source)
			if err != nil {
				return err
			}

			dest := filepath.Join(projectDir, ".rubichan", "skills", manifest.Name)
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

const instructionSkillCreateTemplate = `---
name: %s
version: 0.1.0
description: "A new instruction skill"
---

# Instructions

Add concise guidance for when and how to use this skill.
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
		Long:  "Create a new skill directory with a template manifest.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			skillType, _ := cmd.Flags().GetString("type")

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

			switch skillType {
			case "", "tool":
				manifestContent := fmt.Sprintf(skillCreateTemplate, name)
				if err := os.WriteFile(
					filepath.Join(skillDir, "SKILL.yaml"),
					[]byte(manifestContent), 0o644,
				); err != nil {
					return fmt.Errorf("writing SKILL.yaml: %w", err)
				}

				starContent := fmt.Sprintf(skillStarTemplate, name)
				if err := os.WriteFile(
					filepath.Join(skillDir, "skill.star"),
					[]byte(starContent), 0o644,
				); err != nil {
					return fmt.Errorf("writing skill.star: %w", err)
				}
			case "instruction":
				manifestContent := fmt.Sprintf(instructionSkillCreateTemplate, name)
				if err := os.WriteFile(
					filepath.Join(skillDir, "SKILL.md"),
					[]byte(manifestContent), 0o644,
				); err != nil {
					return fmt.Errorf("writing SKILL.md: %w", err)
				}
			default:
				return fmt.Errorf("unsupported skill type %q", skillType)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Created skill %q in %s\n", name, skillDir)
			return nil
		},
	}
	cmd.Flags().String("dir", "", "output parent directory (default: current working directory)")
	cmd.Flags().String("type", "tool", "skill template type: tool or instruction")
	return cmd
}

func skillTestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test <path>",
		Short: "Validate a skill manifest",
		Long:  "Read and validate the SKILL.yaml or SKILL.md from the given skill directory.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			skillPath := args[0]

			manifest, _, _, err := loadSkillManifest(skillPath)
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

func skillLintCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lint <path>",
		Short: "Lint a skill directory",
		Long:  "Validate a skill directory for authoring issues such as unknown frontmatter keys, duplicate names, oversized instruction bodies, and missing references.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			skillPath := args[0]
			issues := skills.LintSkillDir(skillPath)
			if len(issues) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Skill %q passed lint\n", skillPath)
				return nil
			}

			for _, issue := range issues {
				fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", issue)
			}
			return fmt.Errorf("skill lint failed with %d issue(s)", len(issues))
		},
	}
	return cmd
}

func skillDevCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev <path>",
		Short: "Watch and validate a skill while authoring",
		Long:  "Validate a skill directory once or poll for file changes and rerun manifest validation plus lint checks.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			skillPath := args[0]
			once, _ := cmd.Flags().GetBool("once")
			interval, _ := cmd.Flags().GetDuration("interval")
			if interval <= 0 {
				interval = 2 * time.Second
			}

			snapshot, err := skillDirSnapshot(skillPath)
			if err != nil {
				return err
			}
			if err := runSkillDevCheck(cmd, skillPath); err != nil && once {
				return err
			}
			if once {
				return nil
			}

			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			fmt.Fprintf(cmd.OutOrStdout(), "Watching %q every %s\n", skillPath, interval)
			for {
				select {
				case <-cmd.Context().Done():
					return nil
				case <-ticker.C:
					next, err := skillDirSnapshot(skillPath)
					if err != nil {
						fmt.Fprintf(cmd.OutOrStdout(), "watch error: %v\n", err)
						continue
					}
					if next == snapshot {
						continue
					}
					snapshot = next
					fmt.Fprintf(cmd.OutOrStdout(), "\nChange detected at %s\n", time.Now().Format(time.RFC3339))
					_ = runSkillDevCheck(cmd, skillPath)
				}
			}
		},
	}
	cmd.Flags().Bool("once", false, "run a single validation pass and exit")
	cmd.Flags().Duration("interval", 2*time.Second, "poll interval for change detection")
	return cmd
}

func runSkillDevCheck(cmd *cobra.Command, skillPath string) error {
	manifest, _, kind, err := loadSkillManifest(skillPath)
	out := cmd.OutOrStdout()
	if err != nil {
		fmt.Fprintf(out, "[manifest] invalid: %v\n", err)
		return err
	}
	fmt.Fprintf(out, "[manifest] ok: %s v%s (%s)\n", manifest.Name, manifest.Version, kind)

	issues := skills.LintSkillDir(skillPath)
	if len(issues) == 0 {
		fmt.Fprintln(out, "[lint] ok")
		return nil
	}

	fmt.Fprintf(out, "[lint] %d issue(s)\n", len(issues))
	for _, issue := range issues {
		fmt.Fprintf(out, "- %s\n", issue)
	}
	return fmt.Errorf("skill dev found %d issue(s)", len(issues))
}

func skillDirSnapshot(skillPath string) (string, error) {
	var b strings.Builder
	err := filepath.WalkDir(skillPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(skillPath, path)
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s|%d|%d|%d\n", rel, info.Size(), info.Mode(), info.ModTime().UnixNano())
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("snapshot skill directory: %w", err)
	}
	return b.String(), nil
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
