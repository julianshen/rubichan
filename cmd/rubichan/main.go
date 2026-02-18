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
	"github.com/julianshen/rubichan/internal/integrations"
	"github.com/julianshen/rubichan/internal/output"
	"github.com/julianshen/rubichan/internal/pipeline"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/runner"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/skills/goplugin"
	"github.com/julianshen/rubichan/internal/skills/process"
	"github.com/julianshen/rubichan/internal/skills/sandbox"
	starengine "github.com/julianshen/rubichan/internal/skills/starlark"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/internal/tui"
	"github.com/julianshen/rubichan/pkg/skillsdk"

	// Register providers via init() side effects.
	_ "github.com/julianshen/rubichan/internal/provider/anthropic"
	_ "github.com/julianshen/rubichan/internal/provider/ollama"
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
// --skills and --approve-skills flags. The provider and config are used to
// create integration adapters (LLM completer, HTTP fetcher, git runner,
// skill invoker) that get injected into skill backends. Returns nil if no
// skills are requested.
func createSkillRuntime(registry *tools.Registry, p provider.LLMProvider, cfg *config.Config) (*skills.Runtime, error) {
	skillNames := parseSkillsFlag(skillsFlag)
	if len(skillNames) == 0 {
		return nil, nil
	}

	// Determine user config directory.
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}
	configDir := filepath.Join(home, ".config", "rubichan")

	// Ensure config directory exists for the database file.
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating config directory: %w", err)
	}

	// Use persistent SQLite store so skill approvals survive across sessions.
	dbPath := filepath.Join(configDir, "skills.db")
	s, err := store.NewStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("creating skill store: %w", err)
	}

	userDir := filepath.Join(configDir, "skills")

	// Project-level skill directory.
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getting working directory: %w", err)
	}
	projectDir := filepath.Join(cwd, ".rubichan", "skills")

	loader := skills.NewLoader(userDir, projectDir)
	loader.AddMCPServers(cfg.MCP.Servers)

	// Create integration objects shared across all skill backends.
	llmCompleter := integrations.NewLLMCompleter(p, cfg.Provider.Model)
	httpFetcher := integrations.NewHTTPFetcher(30 * time.Second)
	gitRunner := integrations.NewGitRunner(cwd)

	// SkillInvoker needs the runtime, which we haven't created yet. Create it
	// with nil and set the invoker after runtime creation to break the cycle.
	skillInvoker := integrations.NewSkillInvoker(nil)

	// Create adapters that bridge integrations to backend-specific interfaces.
	starlarkGitAdapter := &starlarkGitRunnerAdapter{runner: gitRunner}
	pluginLLMAdapter := &pluginLLMCompleterAdapter{completer: llmCompleter}
	pluginHTTPAdapter := &pluginHTTPFetcherAdapter{fetcher: httpFetcher}
	pluginGitAdapter := &pluginGitRunnerAdapter{runner: gitRunner}
	pluginSkillAdapter := &pluginSkillInvokerAdapter{invoker: skillInvoker}

	// Backend factory routes to real Starlark, Go plugin, or process backends
	// with integration objects injected.
	backendFactory := func(manifest skills.SkillManifest, dir string) (skills.SkillBackend, error) {
		switch manifest.Implementation.Backend {
		case skills.BackendStarlark:
			engine := starengine.NewEngine(manifest.Name, dir, nil)
			engine.SetLLMCompleter(llmCompleter)
			engine.SetHTTPFetcher(httpFetcher)
			engine.SetGitRunner(starlarkGitAdapter)
			engine.SetSkillInvoker(skillInvoker)
			return engine, nil

		case skills.BackendPlugin:
			return goplugin.NewGoPluginBackend(
				goplugin.WithSkillDir(dir),
				goplugin.WithLLMCompleter(pluginLLMAdapter),
				goplugin.WithHTTPFetcher(pluginHTTPAdapter),
				goplugin.WithGitRunner(pluginGitAdapter),
				goplugin.WithSkillInvoker(pluginSkillAdapter),
			), nil

		case skills.BackendProcess:
			return process.NewProcessBackend(), nil

		default:
			return nil, fmt.Errorf("backend %q not implemented", manifest.Implementation.Backend)
		}
	}

	sandboxFactory := func(skillName string, declared []skills.Permission) skills.PermissionChecker {
		return sandbox.New(s, skillName, declared, sandbox.DefaultPolicy())
	}

	// If --approve-skills is set, auto-approve all requested skills.
	var autoApproveSkills []string
	if approveSkillsFlag {
		autoApproveSkills = skillNames
	}

	rt := skills.NewRuntime(loader, s, registry, autoApproveSkills, backendFactory, sandboxFactory)

	// Now that the runtime exists, wire the SkillInvoker to close the circular
	// dependency. The invoker delegates to rt.InvokeWorkflow.
	skillInvoker.SetInvoker(rt)

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

// loadConfig resolves the config path, loads the config, and applies any
// flag overrides. This eliminates duplication between runInteractive and
// runHeadless.
func loadConfig() (*config.Config, error) {
	cfgPath := configPath
	if cfgPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("cannot determine home directory: %w", err)
		}
		cfgPath = filepath.Join(home, ".config", "rubichan", "config.toml")
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	if modelFlag != "" {
		cfg.Provider.Model = modelFlag
	}
	if providerFlag != "" {
		cfg.Provider.Default = providerFlag
	}

	return cfg, nil
}

func runInteractive() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
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
	rt, err := createSkillRuntime(registry, p, cfg)
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
	cfg, err := loadConfig()
	if err != nil {
		return err
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
	rt, err := createSkillRuntime(registry, p, cfg)
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

// --- Adapter types ---
//
// These adapters bridge the integrations package (which uses context.Context
// and its own struct types) to the backend-specific interfaces (which may
// omit context or use different struct types).

// starlarkGitRunnerAdapter bridges integrations.GitRunner to the
// starlark.GitRunner interface. The Diff method passes through directly;
// Log and Status convert between struct types.
type starlarkGitRunnerAdapter struct {
	runner *integrations.GitRunner
}

func (a *starlarkGitRunnerAdapter) Diff(ctx context.Context, args ...string) (string, error) {
	return a.runner.Diff(ctx, args...)
}

func (a *starlarkGitRunnerAdapter) Log(ctx context.Context, args ...string) ([]starengine.GitLogEntry, error) {
	commits, err := a.runner.Log(ctx, args...)
	if err != nil {
		return nil, err
	}
	entries := make([]starengine.GitLogEntry, len(commits))
	for i, c := range commits {
		entries[i] = starengine.GitLogEntry{Hash: c.Hash, Author: c.Author, Message: c.Message}
	}
	return entries, nil
}

func (a *starlarkGitRunnerAdapter) Status(ctx context.Context) ([]starengine.GitStatusEntry, error) {
	statuses, err := a.runner.Status(ctx)
	if err != nil {
		return nil, err
	}
	entries := make([]starengine.GitStatusEntry, len(statuses))
	for i, s := range statuses {
		entries[i] = starengine.GitStatusEntry{Path: s.Path, Status: s.Status}
	}
	return entries, nil
}

// pluginLLMCompleterAdapter bridges integrations.LLMCompleter to the
// goplugin.PluginLLMCompleter interface (no context parameter).
type pluginLLMCompleterAdapter struct {
	completer *integrations.LLMCompleter
}

func (a *pluginLLMCompleterAdapter) Complete(prompt string) (string, error) {
	return a.completer.Complete(context.Background(), prompt)
}

// pluginHTTPFetcherAdapter bridges integrations.HTTPFetcher to the
// goplugin.PluginHTTPFetcher interface (no context parameter).
type pluginHTTPFetcherAdapter struct {
	fetcher *integrations.HTTPFetcher
}

func (a *pluginHTTPFetcherAdapter) Fetch(url string) (string, error) {
	return a.fetcher.Fetch(context.Background(), url)
}

// pluginGitRunnerAdapter bridges integrations.GitRunner to the
// goplugin.PluginGitRunner interface (no context, skillsdk types).
type pluginGitRunnerAdapter struct {
	runner *integrations.GitRunner
}

func (a *pluginGitRunnerAdapter) Diff(args ...string) (string, error) {
	return a.runner.Diff(context.Background(), args...)
}

func (a *pluginGitRunnerAdapter) Log(args ...string) ([]skillsdk.GitCommit, error) {
	commits, err := a.runner.Log(context.Background(), args...)
	if err != nil {
		return nil, err
	}
	entries := make([]skillsdk.GitCommit, len(commits))
	for i, c := range commits {
		entries[i] = skillsdk.GitCommit{Hash: c.Hash, Author: c.Author, Message: c.Message}
	}
	return entries, nil
}

func (a *pluginGitRunnerAdapter) Status() ([]skillsdk.GitFileStatus, error) {
	statuses, err := a.runner.Status(context.Background())
	if err != nil {
		return nil, err
	}
	entries := make([]skillsdk.GitFileStatus, len(statuses))
	for i, s := range statuses {
		entries[i] = skillsdk.GitFileStatus{Path: s.Path, Status: s.Status}
	}
	return entries, nil
}

// pluginSkillInvokerAdapter bridges integrations.SkillInvoker to the
// goplugin.PluginSkillInvoker interface (no context parameter).
type pluginSkillInvokerAdapter struct {
	invoker *integrations.SkillInvoker
}

func (a *pluginSkillInvokerAdapter) Invoke(name string, input map[string]any) (map[string]any, error) {
	return a.invoker.Invoke(context.Background(), name, input)
}
