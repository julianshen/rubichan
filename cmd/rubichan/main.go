// cmd/rubichan/main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/output"
	"github.com/julianshen/rubichan/internal/pipeline"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/runner"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/internal/tui"

	// Register providers via init() side effects.
	_ "github.com/julianshen/rubichan/internal/provider/anthropic"
	_ "github.com/julianshen/rubichan/internal/provider/openai"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"

	configPath   string
	modelFlag    string
	providerFlag string
	autoApprove  bool

	headless     bool
	promptFlag   string
	fileFlag     string
	modeFlag     string
	outputFlag   string
	diffFlag     string
	maxTurnsFlag int
	timeoutFlag  time.Duration
	toolsFlag    string

	skillsFlag        string
	approveSkillsFlag bool
)

func versionString() string {
	return fmt.Sprintf("rubichan %s (commit: %s, built: %s)", version, commit, date)
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "rubichan",
		Short: "An AI coding assistant",
		Long:  "rubichan — an interactive AI coding assistant powered by LLMs.",
		RunE: func(_ *cobra.Command, _ []string) error {
			if headless {
				return runHeadless()
			}
			return runInteractive()
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "path to config file")
	rootCmd.PersistentFlags().StringVar(&modelFlag, "model", "", "override model name")
	rootCmd.PersistentFlags().StringVar(&providerFlag, "provider", "", "override provider name")
	rootCmd.PersistentFlags().BoolVar(&autoApprove, "auto-approve", false, "auto-approve all tool calls (dangerous: enables RCE)")
	rootCmd.PersistentFlags().BoolVar(&headless, "headless", false, "run in non-interactive headless mode")
	rootCmd.PersistentFlags().StringVar(&promptFlag, "prompt", "", "prompt text for headless mode")
	rootCmd.PersistentFlags().StringVar(&fileFlag, "file", "", "read prompt from file for headless mode")
	rootCmd.PersistentFlags().StringVar(&modeFlag, "mode", "", "headless mode (e.g. code-review)")
	rootCmd.PersistentFlags().StringVar(&outputFlag, "output", "markdown", "output format: json, markdown")
	rootCmd.PersistentFlags().StringVar(&diffFlag, "diff", "", "git diff range for code-review mode")
	rootCmd.PersistentFlags().IntVar(&maxTurnsFlag, "max-turns", 0, "override max agent turns")
	rootCmd.PersistentFlags().DurationVar(&timeoutFlag, "timeout", 120*time.Second, "headless execution timeout")
	rootCmd.PersistentFlags().StringVar(&toolsFlag, "tools", "", "comma-separated tool whitelist (empty = all)")
	rootCmd.PersistentFlags().StringVar(&skillsFlag, "skills", "", "comma-separated list of skill names to activate")
	rootCmd.PersistentFlags().BoolVar(&approveSkillsFlag, "approve-skills", false, "auto-approve skill permissions")

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println(versionString())
		},
	}

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(skillCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// parseSkillsFlag splits a comma-separated skills string into a slice of names.
// Returns nil if the input is empty.
func parseSkillsFlag(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var names []string
	for _, name := range strings.Split(s, ",") {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			names = append(names, trimmed)
		}
	}
	return names
}

// createSkillRuntime creates and configures a skill runtime from the
// --skills and --approve-skills flags. Returns nil if no skills are requested.
func createSkillRuntime(registry *tools.Registry) (*skills.Runtime, error) {
	skillNames := parseSkillsFlag(skillsFlag)
	if len(skillNames) == 0 {
		return nil, nil
	}

	// Create in-memory store for skill state.
	s, err := store.NewStore(":memory:")
	if err != nil {
		return nil, fmt.Errorf("creating skill store: %w", err)
	}

	// Determine user skill directory.
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}
	userDir := filepath.Join(home, ".config", "rubichan", "skills")

	// Project-level skill directory.
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getting working directory: %w", err)
	}
	projectDir := filepath.Join(cwd, ".rubichan", "skills")

	loader := skills.NewLoader(userDir, projectDir)

	// TODO: Wire real backend factory (Starlark, Go plugin, process) based on
	// manifest.Implementation.Backend. For now, return an error to indicate
	// that backend creation is not yet implemented.
	backendFactory := func(manifest skills.SkillManifest) (skills.SkillBackend, error) {
		return nil, fmt.Errorf("backend %q not yet implemented", manifest.Implementation.Backend)
	}

	// TODO: Wire real sandbox factory via skills/sandbox package.
	// WARNING: This is a placeholder that approves all permissions unconditionally.
	// All skill permission enforcement is bypassed until sandbox.New() is wired.
	fmt.Fprintln(os.Stderr, "WARNING: skill permissions are not enforced (sandbox not yet wired)")
	sandboxFactory := func(_ string, _ []skills.Permission) skills.PermissionChecker {
		return &noopPermissionChecker{}
	}

	// If --approve-skills is set, auto-approve all requested skills.
	var autoApproveSkills []string
	if approveSkillsFlag {
		autoApproveSkills = skillNames
	}

	rt := skills.NewRuntime(loader, s, registry, autoApproveSkills, backendFactory, sandboxFactory)

	// Discover skills from all sources.
	if err := rt.Discover(skillNames); err != nil {
		return nil, fmt.Errorf("discovering skills: %w", err)
	}

	// Evaluate triggers and activate matching skills.
	triggerCtx := skills.TriggerContext{
		// TODO: Populate with actual project files, keywords, etc.
	}
	if err := rt.EvaluateAndActivate(triggerCtx); err != nil {
		return nil, fmt.Errorf("activating skills: %w", err)
	}

	return rt, nil
}

// noopPermissionChecker is a placeholder that approves all permissions.
// TODO: Replace with real sandbox.New() integration.
type noopPermissionChecker struct{}

func (n *noopPermissionChecker) CheckPermission(_ skills.Permission) error { return nil }
func (n *noopPermissionChecker) CheckRateLimit(_ string) error             { return nil }
func (n *noopPermissionChecker) ResetTurnLimits()                          {}

func runInteractive() error {
	// Determine config path
	cfgPath := configPath
	if cfgPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot determine home directory: %w", err)
		}
		cfgPath = filepath.Join(home, ".config", "rubichan", "config.toml")
	}

	// Load config
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Apply flag overrides
	if modelFlag != "" {
		cfg.Provider.Model = modelFlag
	}
	if providerFlag != "" {
		cfg.Provider.Default = providerFlag
	}

	// Create provider
	p, err := provider.NewProvider(cfg)
	if err != nil {
		return fmt.Errorf("creating provider: %w", err)
	}

	// Create tool registry
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	registry := tools.NewRegistry()
	if err := registry.Register(tools.NewFileTool(cwd)); err != nil {
		return fmt.Errorf("registering file tool: %w", err)
	}
	if err := registry.Register(tools.NewShellTool(cwd, 120*time.Second)); err != nil {
		return fmt.Errorf("registering shell tool: %w", err)
	}

	// Deny tool calls by default; require explicit --auto-approve flag to skip approval.
	// TODO: replace with TUI-based interactive approval prompt.
	approvalFunc := func(_ context.Context, _ string, _ json.RawMessage) (bool, error) {
		return autoApprove, nil
	}

	// Create skill runtime if --skills is provided.
	var opts []agent.AgentOption
	rt, err := createSkillRuntime(registry)
	if err != nil {
		return fmt.Errorf("creating skill runtime: %w", err)
	}
	if rt != nil {
		opts = append(opts, agent.WithSkillRuntime(rt))
	}

	// Create agent
	a := agent.New(p, registry, approvalFunc, cfg, opts...)

	// Create TUI model and run
	model := tui.NewModel(a, "rubichan", cfg.Provider.Model)
	prog := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		return fmt.Errorf("running TUI: %w", err)
	}

	return nil
}

func runHeadless() error {
	cfgPath := configPath
	if cfgPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot determine home directory: %w", err)
		}
		cfgPath = filepath.Join(home, ".config", "rubichan", "config.toml")
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if modelFlag != "" {
		cfg.Provider.Model = modelFlag
	}
	if providerFlag != "" {
		cfg.Provider.Default = providerFlag
	}
	if maxTurnsFlag > 0 {
		cfg.Agent.MaxTurns = maxTurnsFlag
	}

	// Single top-level timeout governs the entire headless execution.
	ctx, cancel := context.WithTimeout(context.Background(), timeoutFlag)
	defer cancel()

	// Resolve input: code-review mode builds prompt from diff, others need explicit input.
	var promptText string
	if modeFlag == "code-review" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		diff, err := pipeline.ExtractDiff(ctx, cwd, diffFlag)
		if err != nil {
			return fmt.Errorf("extracting diff: %w", err)
		}
		promptText = pipeline.BuildReviewPrompt(diff)
	} else {
		var stdinReader io.Reader
		if stat, err := os.Stdin.Stat(); err == nil && stat.Mode()&os.ModeCharDevice == 0 {
			stdinReader = os.Stdin
		}

		var err error
		promptText, err = runner.ResolveInput(promptFlag, fileFlag, stdinReader)
		if err != nil {
			return err
		}
	}

	// Create provider
	p, err := provider.NewProvider(cfg)
	if err != nil {
		return fmt.Errorf("creating provider: %w", err)
	}

	// Create tool registry with optional whitelist
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	registry := tools.NewRegistry()
	allowed := parseToolsFlag(toolsFlag)

	if shouldRegister("file", allowed) {
		if err := registry.Register(tools.NewFileTool(cwd)); err != nil {
			return fmt.Errorf("registering file tool: %w", err)
		}
	}
	if shouldRegister("shell", allowed) {
		if err := registry.Register(tools.NewShellTool(cwd, timeoutFlag)); err != nil {
			return fmt.Errorf("registering shell tool: %w", err)
		}
	}

	// Headless always auto-approves (tools are restricted via whitelist)
	approvalFunc := func(_ context.Context, _ string, _ json.RawMessage) (bool, error) {
		return true, nil
	}

	// Create skill runtime if --skills is provided.
	var opts []agent.AgentOption
	rt, err := createSkillRuntime(registry)
	if err != nil {
		return fmt.Errorf("creating skill runtime: %w", err)
	}
	if rt != nil {
		opts = append(opts, agent.WithSkillRuntime(rt))
	}

	a := agent.New(p, registry, approvalFunc, cfg, opts...)

	// Run headless
	mode := modeFlag
	if mode == "" {
		mode = "generic"
	}

	hr := runner.NewHeadlessRunner(a.Turn)
	result, err := hr.Run(ctx, promptText, mode)
	if err != nil {
		return err
	}

	// Format output
	var formatter output.Formatter
	switch outputFlag {
	case "json":
		formatter = output.NewJSONFormatter()
	default:
		formatter = output.NewMarkdownFormatter()
	}

	out, err := formatter.Format(result)
	if err != nil {
		return fmt.Errorf("formatting output: %w", err)
	}

	fmt.Print(string(out))

	if result.Error != "" {
		return fmt.Errorf("agent run failed: %s", result.Error)
	}

	return nil
}

// parseToolsFlag splits a comma-separated tools string into a set.
// Returns nil if the input is empty (meaning all tools allowed).
func parseToolsFlag(s string) map[string]bool {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	m := make(map[string]bool)
	for _, t := range strings.Split(s, ",") {
		if name := strings.TrimSpace(t); name != "" {
			m[name] = true
		}
	}
	return m
}

// shouldRegister returns true if the tool should be registered.
// If allowed is nil, all tools are allowed.
func shouldRegister(name string, allowed map[string]bool) bool {
	if allowed == nil {
		return true
	}
	return allowed[name]
}
