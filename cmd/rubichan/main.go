// cmd/rubichan/main.go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/sourcegraph/conc"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/integrations"
	"github.com/julianshen/rubichan/internal/output"
	"github.com/julianshen/rubichan/internal/pipeline"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/runner"
	"github.com/julianshen/rubichan/internal/security"
	"github.com/julianshen/rubichan/internal/security/scanner"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/skills/builtin/appledev"
	"github.com/julianshen/rubichan/internal/skills/goplugin"
	"github.com/julianshen/rubichan/internal/skills/mcpbackend"
	"github.com/julianshen/rubichan/internal/skills/process"
	"github.com/julianshen/rubichan/internal/skills/sandbox"
	starengine "github.com/julianshen/rubichan/internal/skills/starlark"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/internal/tools/xcode"
	"github.com/julianshen/rubichan/internal/tui"
	"github.com/julianshen/rubichan/pkg/skillsdk"

	// Register providers via init() side effects.
	_ "github.com/julianshen/rubichan/internal/provider/anthropic"
	"github.com/julianshen/rubichan/internal/provider/ollama"
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

	resumeFlag string
	failOnFlag string
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
	rootCmd.PersistentFlags().StringVar(&resumeFlag, "resume", "", "resume a previous session by ID")
	rootCmd.PersistentFlags().StringVar(&failOnFlag, "fail-on", "", "exit non-zero if findings at/above severity (critical, high, medium, low)")

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println(versionString())
		},
	}

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(skillCmd())
	rootCmd.AddCommand(wikiCmd())
	rootCmd.AddCommand(ollamaCmd())

	if err := rootCmd.Execute(); err != nil {
		var exitErr *runner.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
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
// skill invoker) that get injected into skill backends.
//
// The returned io.Closer must be closed by the caller to release the SQLite
// store. Returns (nil, nil, nil) if no skills are requested.
func createSkillRuntime(ctx context.Context, registry *tools.Registry, p provider.LLMProvider, cfg *config.Config) (*skills.Runtime, io.Closer, error) {
	skillNames := parseSkillsFlag(skillsFlag)
	if len(skillNames) == 0 {
		return nil, nil, nil
	}

	if cfg == nil {
		return nil, nil, fmt.Errorf("config is required for skill runtime")
	}

	// Determine user config directory.
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, fmt.Errorf("cannot determine home directory: %w", err)
	}
	configDir := filepath.Join(home, ".config", "rubichan")

	// Ensure config directory exists for the database file.
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("creating config directory: %w", err)
	}

	// Use persistent SQLite store so skill approvals survive across sessions.
	dbPath := filepath.Join(configDir, "skills.db")
	s, err := store.NewStore(dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("creating skill store: %w", err)
	}

	userDir := filepath.Join(configDir, "skills")

	// Project-level skill directory.
	cwd, err := os.Getwd()
	if err != nil {
		s.Close()
		return nil, nil, fmt.Errorf("getting working directory: %w", err)
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
	// Plugin adapters store the parent context so cancellation propagates
	// through to LLM/HTTP/Git calls made from Go plugins.
	starlarkGitAdapter := &starlarkGitRunnerAdapter{runner: gitRunner}
	pluginLLMAdapter := &pluginLLMCompleterAdapter{ctx: ctx, completer: llmCompleter}
	pluginHTTPAdapter := &pluginHTTPFetcherAdapter{ctx: ctx, fetcher: httpFetcher}
	pluginGitAdapter := &pluginGitRunnerAdapter{ctx: ctx, runner: gitRunner}
	pluginSkillAdapter := &pluginSkillInvokerAdapter{ctx: ctx, invoker: skillInvoker}

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

		case skills.BackendMCP:
			// Derive MCP server name from manifest name by stripping the "mcp-" prefix
			// added during discovery in loader.go.
			mcpServerName := strings.TrimPrefix(manifest.Name, "mcp-")
			return mcpbackend.NewMCPBackendFromConfig(
				ctx,
				mcpServerName,
				manifest.Implementation.MCPTransport,
				manifest.Implementation.MCPCommand,
				manifest.Implementation.MCPArgs,
				manifest.Implementation.MCPURL,
			)

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
		s.Close()
		return nil, nil, fmt.Errorf("discovering skills: %w", err)
	}

	// Evaluate triggers and activate matching skills.
	triggerCtx := skills.TriggerContext{
		// TODO: Populate with actual project files, keywords, etc.
	}
	if err := rt.EvaluateAndActivate(triggerCtx); err != nil {
		s.Close()
		return nil, nil, fmt.Errorf("activating skills: %w", err)
	}

	return rt, s, nil
}

// openStore opens (or creates) the conversation persistence database in the
// given config directory. It creates the directory if it doesn't exist.
func openStore(configDir string) (*store.Store, error) {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating config directory: %w", err)
	}
	dbPath := filepath.Join(configDir, "rubichan.db")
	return store.NewStore(dbPath)
}

// configDir returns the resolved rubichan config directory (~/.config/rubichan).
func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "rubichan"), nil
}

// newDefaultSecurityEngine creates a security engine pre-configured with all
// built-in static scanners. This lives in main.go to avoid an import cycle
// between security/ and security/scanner/.
func newDefaultSecurityEngine(cfg security.EngineConfig) *security.Engine {
	e := security.NewEngine(cfg)
	e.AddScanner(scanner.NewSecretScanner())
	e.AddScanner(scanner.NewSASTScanner())
	e.AddScanner(scanner.NewConfigScanner())
	e.AddScanner(scanner.NewDepScanner(nil))
	e.AddScanner(scanner.NewLicenseScanner())
	e.AddScanner(scanner.NewAppleScanner())
	return e
}

// autoDetectProvider checks if Ollama should be auto-selected.
// Returns true if provider was switched to Ollama.
func autoDetectProvider(cfg *config.Config, providerFlagValue, ollamaURL string) bool {
	// Skip if provider explicitly set via flag.
	if providerFlagValue != "" {
		return false
	}
	// Only auto-detect when using default provider (anthropic).
	if cfg.Provider.Default != "anthropic" {
		return false
	}
	// Skip if Anthropic API key is configured.
	if cfg.Provider.Anthropic.APIKey != "" || os.Getenv("ANTHROPIC_API_KEY") != "" {
		return false
	}

	client := ollama.NewClient(ollamaURL)
	if client.IsRunning(context.Background()) {
		cfg.Provider.Default = "ollama"
		return true
	}
	return false
}

// resolveOllamaModel queries Ollama for available models and resolves which
// model to use. With a single model it auto-selects; with zero models it
// returns an error. The ollamaURL parameter allows testing with httptest.
func resolveOllamaModel(ollamaURL string) (string, error) {
	client := ollama.NewClient(ollamaURL)
	models, err := client.ListModels(context.Background())
	if err != nil {
		return "", fmt.Errorf("listing Ollama models: %w", err)
	}

	if len(models) == 0 {
		return "", fmt.Errorf("no models found; run 'rubichan ollama pull <model>' first")
	}

	if len(models) == 1 {
		return models[0].Name, nil
	}

	// Multiple models — in interactive mode, we'd show a picker.
	// For now, return the first model. The TUI picker integration
	// requires running a Bubble Tea program which is complex to wire here.
	// TODO: integrate tui.ModelPicker when running interactively.
	return models[0].Name, nil
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

	// Resolve Ollama URL once for all downstream checks.
	ollamaURL := cfg.Provider.Ollama.BaseURL
	if ollamaURL == "" {
		ollamaURL = ollama.DefaultBaseURL
	}

	// Auto-detect Ollama if no API key and no explicit provider.
	if autoDetectProvider(cfg, providerFlag, ollamaURL) {
		fmt.Fprintln(os.Stderr, "Using local Ollama (no API key configured)")
	}

	// Resolve Ollama model if provider is ollama and no model specified.
	if cfg.Provider.Default == "ollama" && cfg.Provider.Model == "" {
		model, err := resolveOllamaModel(ollamaURL)
		if err != nil {
			return nil, err
		}
		cfg.Provider.Model = model
		fmt.Fprintf(os.Stderr, "Using Ollama model: %s\n", model)
	}

	return cfg, nil
}

func runInteractive() error {
	// Resolve config path early for bootstrap check.
	cfgDir, err := configDir()
	if err != nil {
		return err
	}
	cfgPath := configPath
	if cfgPath == "" {
		cfgPath = filepath.Join(cfgDir, "config.toml")
	}

	// Run first-run bootstrap wizard if no config/API key found.
	if tui.NeedsBootstrap(cfgPath) {
		wizard := tui.NewBootstrapForm(cfgPath)
		form := wizard.Form()
		form.SubmitCmd = tea.Quit
		form.CancelCmd = tea.Interrupt
		prog := tea.NewProgram(form)
		finalModel, err := prog.Run()
		if err != nil {
			return fmt.Errorf("bootstrap wizard: %w", err)
		}
		// Bubble Tea returns the final form model after all Updates.
		// We must check state on the returned model, not the original,
		// because huh.Form.Update returns new instances.
		if f, ok := finalModel.(*huh.Form); ok {
			wizard.SetForm(f)
		}
		if wizard.IsAborted() {
			return fmt.Errorf("setup cancelled")
		}
		if wizard.IsCompleted() {
			if err := wizard.Save(); err != nil {
				return fmt.Errorf("saving bootstrap config: %w", err)
			}
		}
	}

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
	if err := registry.Register(tools.NewSearchTool(cwd)); err != nil {
		return fmt.Errorf("registering search tool: %w", err)
	}

	// Auto-activate apple-dev skill if Apple project detected.
	var opts []agent.AgentOption
	if appleOpt, err := wireAppleDev(cwd, registry, nil); err != nil {
		return err
	} else if appleOpt != nil {
		opts = append(opts, appleOpt)
		// Prevent createSkillRuntime from trying to discover apple-dev again.
		skillsFlag = removeSkill("apple-dev", skillsFlag)
	}

	// Build the approval function. When --auto-approve is set, skip the TUI
	// prompt entirely. Otherwise, defer to the TUI model's interactive prompt.
	// The model is created first (with nil agent) so we can extract its
	// approval function before constructing the agent.
	var approvalFunc agent.ApprovalFunc
	if autoApprove {
		approvalFunc = func(_ context.Context, _ string, _ json.RawMessage) (bool, error) {
			return true, nil
		}
	}

	// Wire conversation persistence.
	s, err := openStore(cfgDir)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}
	defer s.Close()
	opts = append(opts, agent.WithStore(s))
	if resumeFlag != "" {
		opts = append(opts, agent.WithResumeSession(resumeFlag))
	}

	// Inject project-level AGENT.md into system prompt.
	agentMD, agentMDErr := config.LoadAgentMD(cwd)
	if agentMDErr != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to load AGENT.md: %v\n", agentMDErr)
	}
	if agentMD != "" {
		opts = append(opts, agent.WithAgentMD(agentMD))
	}

	// Create skill runtime if --skills is provided.
	rt, storeCloser, err := createSkillRuntime(context.Background(), registry, p, cfg)
	if err != nil {
		return fmt.Errorf("creating skill runtime: %w", err)
	}
	if storeCloser != nil {
		defer storeCloser.Close()
	}
	if rt != nil {
		opts = append(opts, agent.WithSkillRuntime(rt))
	}

	// Wire cross-session memory and summarizer.
	summarizer := agent.NewLLMSummarizer(p, cfg.Provider.Model)
	opts = append(opts, agent.WithSummarizer(summarizer))
	opts = append(opts, agent.WithMemoryStore(&storeMemoryAdapter{store: s}))

	// Create TUI model first (with nil agent) so we can extract the
	// interactive approval function before constructing the agent.
	model := tui.NewModel(nil, "rubichan", cfg.Provider.Model, cfg.Agent.MaxTurns, cfgPath, cfg)
	if !autoApprove {
		approvalFunc = model.MakeApprovalFunc()
		opts = append(opts, agent.WithAutoApproveChecker(model))
	} else {
		opts = append(opts, agent.WithAutoApproveChecker(agent.AlwaysAutoApprove{}))
	}

	// Create agent with the approval function.
	a := agent.New(p, registry, approvalFunc, cfg, opts...)

	// Register notes tool backed by agent's scratchpad.
	if err := registry.Register(tools.NewNotesTool(a.ScratchpadAccess())); err != nil {
		return fmt.Errorf("registering notes tool: %w", err)
	}

	// Wire the agent into the TUI model now that both exist.
	model.SetAgent(a)
	prog := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		return fmt.Errorf("running TUI: %w", err)
	}

	// Save memories on graceful shutdown.
	if err := a.SaveMemories(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save memories: %v\n", err)
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
	if shouldRegister("search", allowed) {
		if err := registry.Register(tools.NewSearchTool(cwd)); err != nil {
			return fmt.Errorf("registering search tool: %w", err)
		}
	}

	// Auto-activate apple-dev skill if Apple project detected.
	var opts []agent.AgentOption
	if appleOpt, err := wireAppleDev(cwd, registry, allowed); err != nil {
		return err
	} else if appleOpt != nil {
		opts = append(opts, appleOpt)
		skillsFlag = removeSkill("apple-dev", skillsFlag)
	}

	// Headless always auto-approves (tools are restricted via whitelist)
	approvalFunc := func(_ context.Context, _ string, _ json.RawMessage) (bool, error) {
		return true, nil
	}

	// Wire conversation persistence.
	cfgDir, err := configDir()
	if err != nil {
		return err
	}
	s, err := openStore(cfgDir)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}
	defer s.Close()
	opts = append(opts, agent.WithStore(s))
	if resumeFlag != "" {
		opts = append(opts, agent.WithResumeSession(resumeFlag))
	}

	// Inject project-level AGENT.md into system prompt.
	agentMD, agentMDErr := config.LoadAgentMD(cwd)
	if agentMDErr != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to load AGENT.md: %v\n", agentMDErr)
	}
	if agentMD != "" {
		opts = append(opts, agent.WithAgentMD(agentMD))
	}

	// Create skill runtime if --skills is provided.
	rt, storeCloser, err := createSkillRuntime(ctx, registry, p, cfg)
	if err != nil {
		return fmt.Errorf("creating skill runtime: %w", err)
	}
	if storeCloser != nil {
		defer storeCloser.Close()
	}
	if rt != nil {
		opts = append(opts, agent.WithSkillRuntime(rt))
	}

	// Wire cross-session memory and summarizer.
	headlessSummarizer := agent.NewLLMSummarizer(p, cfg.Provider.Model)
	opts = append(opts, agent.WithSummarizer(headlessSummarizer))
	opts = append(opts, agent.WithMemoryStore(&storeMemoryAdapter{store: s}))

	// Headless always auto-approves, so all tools can run in parallel.
	opts = append(opts, agent.WithAutoApproveChecker(agent.AlwaysAutoApprove{}))

	a := agent.New(p, registry, approvalFunc, cfg, opts...)

	// Register notes tool backed by agent's scratchpad.
	if shouldRegister("notes", allowed) {
		if err := registry.Register(tools.NewNotesTool(a.ScratchpadAccess())); err != nil {
			return fmt.Errorf("registering notes tool: %w", err)
		}
	}

	// Run headless
	mode := modeFlag
	if mode == "" {
		mode = "generic"
	}

	// Run LLM review and security scan concurrently for code-review mode.
	hr := runner.NewHeadlessRunner(a.Turn)
	var result *output.RunResult
	var secReport *security.Report

	if mode == "code-review" {
		engineCfg := security.EngineConfig{
			Concurrency:     4,
			MaxLLMChunks:    cfg.Security.MaxLLMCalls,
			ExcludePatterns: cfg.Security.ExcludePatterns,
		}
		engine := newDefaultSecurityEngine(engineCfg)

		// Load .security.yaml for custom rules.
		projectCfg, projectCfgErr := security.LoadProjectConfig(cwd)
		if projectCfgErr != nil {
			fmt.Fprintf(os.Stderr, "warning: loading .security.yaml: %v\n", projectCfgErr)
		}
		if projectCfg != nil && len(projectCfg.Rules) > 0 {
			engine.AddScanner(scanner.NewCustomRuleScanner(projectCfg.Rules))
		}

		var wg conc.WaitGroup
		wg.Go(func() {
			var runErr error
			result, runErr = hr.Run(ctx, promptText, mode)
			if runErr != nil {
				result = &output.RunResult{Error: runErr.Error(), Mode: mode}
			}
		})
		wg.Go(func() {
			report, scanErr := engine.Run(ctx, security.ScanTarget{RootDir: cwd})
			if scanErr != nil {
				fmt.Fprintf(os.Stderr, "warning: security scan failed: %v\n", scanErr)
				return
			}
			secReport = report
		})
		wg.Wait()

		// Apply severity overrides from .security.yaml after scan completes.
		if secReport != nil && projectCfg != nil && len(projectCfg.Overrides) > 0 {
			security.ApplyOverrides(secReport.Findings, projectCfg.Overrides)
		}
	} else {
		var runErr error
		result, runErr = hr.Run(ctx, promptText, mode)
		if runErr != nil {
			return runErr
		}
	}

	// Merge security findings into result.
	if secReport != nil {
		for _, finding := range secReport.Findings {
			result.SecurityFindings = append(result.SecurityFindings, output.SecurityFinding{
				ID:       finding.ID,
				Scanner:  finding.Scanner,
				Severity: string(finding.Severity),
				Title:    finding.Title,
				File:     finding.Location.File,
				Line:     finding.Location.StartLine,
			})
		}
		summary := secReport.Summary()
		result.SecuritySummary = &output.SecuritySummaryData{
			Critical: summary.Critical,
			High:     summary.High,
			Medium:   summary.Medium,
			Low:      summary.Low,
			Info:     summary.Info,
		}
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

	// Save memories on completion.
	if err := a.SaveMemories(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save memories: %v\n", err)
	}

	if result.Error != "" {
		return fmt.Errorf("agent run failed: %s", result.Error)
	}

	// Compute exit code from security findings.
	failOn := failOnFlag
	if failOn == "" {
		failOn = cfg.Security.FailOn
	}
	if exitCode := runner.ExitCodeFromFindings(result.SecurityFindings, failOn); exitCode != 0 {
		return &runner.ExitError{Code: exitCode}
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

// removeSkill removes a skill name from a comma-separated flag value.
func removeSkill(name, flagValue string) string {
	var kept []string
	for _, s := range strings.Split(flagValue, ",") {
		if strings.TrimSpace(s) != name {
			if trimmed := strings.TrimSpace(s); trimmed != "" {
				kept = append(kept, trimmed)
			}
		}
	}
	return strings.Join(kept, ",")
}

// containsSkill returns true if name appears in the comma-separated flagValue.
func containsSkill(name, flagValue string) bool {
	for _, s := range strings.Split(flagValue, ",") {
		if strings.TrimSpace(s) == name {
			return true
		}
	}
	return false
}

// wireAppleDev detects Apple projects and registers Xcode tools + system prompt.
// The allowed map is used to filter tools in headless mode (nil means all allowed).
// Returns an AgentOption to inject the Apple system prompt, or nil if not activated.
func wireAppleDev(cwd string, registry *tools.Registry, allowed map[string]bool) (agent.AgentOption, error) {
	appleProject := xcode.DiscoverProject(cwd)
	if appleProject.Type == "none" && !containsSkill("apple-dev", skillsFlag) {
		return nil, nil
	}
	pc := xcode.NewRealPlatformChecker()
	appleBackend := &appledev.Backend{WorkDir: cwd, Platform: pc}
	if err := appleBackend.Load(appledev.Manifest(), nil); err != nil {
		return nil, fmt.Errorf("loading apple-dev skill: %w", err)
	}
	registered := 0
	for _, tool := range appleBackend.Tools() {
		if shouldRegister(tool.Name(), allowed) {
			if err := registry.Register(tool); err != nil {
				return nil, fmt.Errorf("registering xcode tool %s: %w", tool.Name(), err)
			}
			registered++
		}
	}
	if registered == 0 {
		return nil, nil
	}
	return agent.WithExtraSystemPrompt("Apple Platform Expertise", appledev.SystemPrompt()), nil
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
// goplugin.PluginLLMCompleter interface. The stored context propagates
// cancellation from the parent (e.g. headless timeout) to LLM calls.
type pluginLLMCompleterAdapter struct {
	ctx       context.Context
	completer *integrations.LLMCompleter
}

func (a *pluginLLMCompleterAdapter) Complete(prompt string) (string, error) {
	return a.completer.Complete(a.ctx, prompt)
}

// pluginHTTPFetcherAdapter bridges integrations.HTTPFetcher to the
// goplugin.PluginHTTPFetcher interface.
type pluginHTTPFetcherAdapter struct {
	ctx     context.Context
	fetcher *integrations.HTTPFetcher
}

func (a *pluginHTTPFetcherAdapter) Fetch(url string) (string, error) {
	return a.fetcher.Fetch(a.ctx, url)
}

// pluginGitRunnerAdapter bridges integrations.GitRunner to the
// goplugin.PluginGitRunner interface (skillsdk types).
type pluginGitRunnerAdapter struct {
	ctx    context.Context
	runner *integrations.GitRunner
}

func (a *pluginGitRunnerAdapter) Diff(args ...string) (string, error) {
	return a.runner.Diff(a.ctx, args...)
}

func (a *pluginGitRunnerAdapter) Log(args ...string) ([]skillsdk.GitCommit, error) {
	commits, err := a.runner.Log(a.ctx, args...)
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
	statuses, err := a.runner.Status(a.ctx)
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
// goplugin.PluginSkillInvoker interface.
type pluginSkillInvokerAdapter struct {
	ctx     context.Context
	invoker *integrations.SkillInvoker
}

func (a *pluginSkillInvokerAdapter) Invoke(name string, input map[string]any) (map[string]any, error) {
	return a.invoker.Invoke(a.ctx, name, input)
}

// storeMemoryAdapter bridges store.Store to agent.MemoryStore interface.
type storeMemoryAdapter struct {
	store *store.Store
}

func (a *storeMemoryAdapter) SaveMemory(workingDir, tag, content string) error {
	return a.store.SaveMemory(workingDir, tag, content)
}

func (a *storeMemoryAdapter) LoadMemories(workingDir string) ([]agent.MemoryEntry, error) {
	storeMemories, err := a.store.LoadMemories(workingDir)
	if err != nil {
		return nil, err
	}
	entries := make([]agent.MemoryEntry, len(storeMemories))
	for i, m := range storeMemories {
		entries[i] = agent.MemoryEntry{Tag: m.Tag, Content: m.Content}
	}
	return entries, nil
}
