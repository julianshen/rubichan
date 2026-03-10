// cmd/rubichan/main.go
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/sourcegraph/conc"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/integrations"
	"github.com/julianshen/rubichan/internal/output"
	"github.com/julianshen/rubichan/internal/persona"
	"github.com/julianshen/rubichan/internal/pipeline"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/runner"
	"github.com/julianshen/rubichan/internal/security"
	"github.com/julianshen/rubichan/internal/security/scanner"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/skills/builtin"
	"github.com/julianshen/rubichan/internal/skills/builtin/appledev"
	"github.com/julianshen/rubichan/internal/skills/builtin/frontenddesign"
	"github.com/julianshen/rubichan/internal/skills/builtin/superpowers"
	"github.com/julianshen/rubichan/internal/skills/builtin/uiuxpromax"
	"github.com/julianshen/rubichan/internal/skills/goplugin"
	"github.com/julianshen/rubichan/internal/skills/mcpbackend"
	"github.com/julianshen/rubichan/internal/skills/process"
	"github.com/julianshen/rubichan/internal/skills/sandbox"
	starengine "github.com/julianshen/rubichan/internal/skills/starlark"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/internal/tools/xcode"
	"github.com/julianshen/rubichan/internal/tui"
	"github.com/julianshen/rubichan/internal/wiki"
	"github.com/julianshen/rubichan/internal/worktree"
	"github.com/julianshen/rubichan/pkg/skillsdk"

	"golang.org/x/term"

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

	resumeFlag   string
	failOnFlag   string
	worktreeFlag string
)

func versionString() string {
	return fmt.Sprintf("rubichan %s (commit: %s, built: %s)", version, commit, date)
}

// setupWorkingDir determines the effective working directory. When --worktree
// is specified, it creates/reuses a worktree and returns a cleanup function
// that auto-removes the worktree if it has no changes. The returned manager
// is non-nil only when a worktree is active.
func setupWorkingDir(cfg *config.Config) (cwd string, mgr *worktree.Manager, cleanup func(), err error) {
	cleanup = func() {} // no-op default

	if worktreeFlag == "" {
		cwd, err = os.Getwd()
		if err != nil {
			return "", nil, nil, fmt.Errorf("getting working directory: %w", err)
		}
		return cwd, nil, cleanup, nil
	}

	out, gitErr := runGitCommand("rev-parse", "--show-toplevel")
	if gitErr != nil {
		return "", nil, nil, fmt.Errorf("not in a git repository: %w", gitErr)
	}
	root := strings.TrimSpace(out)

	wtCfg := worktree.Config{
		MaxWorktrees: cfg.Worktree.MaxCount,
		BaseBranch:   cfg.Worktree.BaseBranch,
		AutoCleanup:  cfg.Worktree.AutoCleanup,
	}
	mgr = worktree.NewManager(root, wtCfg)

	wt, createErr := mgr.Create(context.Background(), worktreeFlag)
	if createErr != nil {
		return "", nil, nil, fmt.Errorf("creating worktree: %w", createErr)
	}

	wtDir := wt.Dir()
	cleanup = func() {
		hasChanges, err := mgr.HasChanges(context.Background(), worktreeFlag)
		if err != nil {
			// Treat status-check failure as dirty — preserve worktree.
			fmt.Fprintf(os.Stderr, "Warning: cannot check worktree %q status: %v (preserving)\n", worktreeFlag, err)
			return
		}
		if hasChanges {
			fmt.Fprintf(os.Stderr, "Worktree %q preserved at %s (has uncommitted changes)\n", worktreeFlag, wtDir)
		} else if cfg.Worktree.AutoCleanup {
			if err := mgr.Remove(context.Background(), worktreeFlag); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to clean up worktree %q: %v\n", worktreeFlag, err)
			}
		}
	}

	return wtDir, mgr, cleanup, nil
}

// runGitCommand executes a git command and returns its stdout.
func runGitCommand(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
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
	rootCmd.PersistentFlags().StringVar(&worktreeFlag, "worktree", "", "run in an isolated git worktree with the given name")

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println(versionString())
		},
	}

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(skillCmd())
	// wiki is now a built-in skill (generate_wiki tool), not a CLI subcommand.
	rootCmd.AddCommand(ollamaCmd())
	rootCmd.AddCommand(worktreeCmd())

	if err := rootCmd.Execute(); err != nil {
		var exitErr *runner.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		fmt.Fprint(os.Stderr, persona.ErrorMessage(err.Error()))
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

// noopPromptBackend is a no-op backend for prompt-only skills that have no
// implementation backend. It satisfies the SkillBackend interface so prompt
// skills can be activated through the normal runtime flow.
type noopPromptBackend struct{}

func (*noopPromptBackend) Load(_ skills.SkillManifest, _ skills.PermissionChecker) error {
	return nil
}
func (*noopPromptBackend) Tools() []tools.Tool                            { return nil }
func (*noopPromptBackend) Hooks() map[skills.HookPhase]skills.HookHandler { return nil }
func (*noopPromptBackend) Commands() []commands.SlashCommand              { return nil }
func (*noopPromptBackend) Agents() []*skills.AgentDefinition              { return nil }
func (*noopPromptBackend) Unload() error                                  { return nil }

// createSkillRuntime creates and configures a skill runtime with built-in
// prompt skills and any explicitly requested skills from --skills flag.
// Built-in skills (superpowers, frontend-design, apple-platform-guide) are
// always registered and auto-activate based on mode triggers.
//
// The mode parameter is set in TriggerContext to enable mode-based activation
// (e.g. "interactive", "headless", "code-review").
//
// The returned io.Closer must be closed by the caller to release the SQLite
// store.
func createSkillRuntime(ctx context.Context, registry *tools.Registry, p provider.LLMProvider, cfg *config.Config, mode string, workDir string) (*skills.Runtime, io.Closer, error) {
	skillNames := parseSkillsFlag(skillsFlag)

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
	if cfg.Skills.UserDir != "" {
		userDir = cfg.Skills.UserDir
	}

	// Project-level skill directory.
	projectDir := filepath.Join(workDir, ".rubichan", "skills")

	loader := skills.NewLoader(userDir, projectDir)
	loader.AddSkillDirs(cfg.Skills.Dirs)
	loader.AddMCPServers(cfg.MCP.Servers)

	// Register built-in prompt skills. These auto-activate via mode triggers.
	if err := registerBuiltinSkillPrompts(loader, configDir); err != nil {
		return nil, nil, fmt.Errorf("register builtin skills: %w", err)
	}

	// Create integration objects shared across all skill backends.
	llmCompleter := integrations.NewLLMCompleter(p, cfg.Provider.Model)
	httpFetcher := integrations.NewHTTPFetcher(30 * time.Second)
	gitRunner := integrations.NewGitRunner(workDir)

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
	// with integration objects injected. Prompt-only skills use a noop backend.
	backendFactory := func(manifest skills.SkillManifest, dir string) (skills.SkillBackend, error) {
		switch manifest.Implementation.Backend {
		case "":
			// Prompt-only skills have no implementation backend.
			return &noopPromptBackend{}, nil
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
	rt.SetActivationThreshold(cfg.Skills.ActivationThreshold)

	// Now that the runtime exists, wire the SkillInvoker to close the circular
	// dependency. The invoker delegates to rt.InvokeWorkflow.
	skillInvoker.SetInvoker(rt)

	// Discover skills from all sources.
	if err := rt.Discover(skillNames); err != nil {
		s.Close()
		return nil, nil, fmt.Errorf("discovering skills: %w", err)
	}

	// Collect top-level project files for trigger evaluation.
	entries, _ := os.ReadDir(workDir)
	projectFiles := make([]string, 0, len(entries))
	for _, e := range entries {
		projectFiles = append(projectFiles, e.Name())
	}

	// Evaluate triggers and activate matching skills.
	triggerCtx := skills.TriggerContext{
		Mode:         mode,
		ProjectFiles: projectFiles,
	}
	if err := rt.EvaluateAndActivate(triggerCtx); err != nil {
		s.Close()
		return nil, nil, fmt.Errorf("activating skills: %w", err)
	}

	return rt, s, nil
}

func registerBuiltinSkillPrompts(loader *skills.Loader, configDir string) error {
	superpowers.Register(loader)
	frontenddesign.Register(loader)
	appledev.RegisterPrompt(loader)
	return uiuxpromax.Register(loader, configDir)
}

func emitSkillDiscoveryWarnings(w io.Writer, rt *skills.Runtime) {
	if w == nil || rt == nil {
		return
	}
	for _, warning := range rt.GetDiscoveryWarnings() {
		fmt.Fprintf(w, "warning: %s\n", warning)
	}
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

func promptFolderAccess(workingDir string, in io.Reader, out io.Writer) (bool, error) {
	if _, err := fmt.Fprintf(out, "Allow rubichan to access this folder?\n  %s\nType 'yes' to continue: ", workingDir); err != nil {
		return false, fmt.Errorf("writing folder access prompt: %w", err)
	}

	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("reading folder access response: %w", err)
	}
	return strings.EqualFold(strings.TrimSpace(line), "yes"), nil
}

func ensureFolderAccessApproved(s *store.Store, workingDir string, in io.Reader, out io.Writer) error {
	approved, err := s.IsFolderApproved(workingDir)
	if err != nil {
		return fmt.Errorf("checking folder access approval: %w", err)
	}
	if approved {
		return nil
	}

	allow, err := promptFolderAccess(workingDir, in, out)
	if err != nil {
		return err
	}
	if !allow {
		return fmt.Errorf("folder access denied by user")
	}

	if err := s.ApproveFolderAccess(workingDir); err != nil {
		return fmt.Errorf("saving folder access approval: %w", err)
	}
	return nil
}

func ensureFolderAccessApprovedNonInteractive(s *store.Store, workingDir string, autoApprove bool) error {
	approved, err := s.IsFolderApproved(workingDir)
	if err != nil {
		return fmt.Errorf("checking folder access approval: %w", err)
	}
	if approved {
		return nil
	}

	if !autoApprove {
		return fmt.Errorf("folder access for %q is not approved; rerun interactively to approve or use --auto-approve", workingDir)
	}

	if err := s.ApproveFolderAccess(workingDir); err != nil {
		return fmt.Errorf("saving folder access approval: %w", err)
	}
	return nil
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

// pipelineComponents holds the pipeline and its key components for
// integration with the approval system.
type pipelineComponents struct {
	Pipeline   *toolexec.Pipeline
	Classifier *toolexec.Classifier
	RuleEngine *toolexec.RuleEngine
}

// buildPipeline constructs a tool execution pipeline from config rules,
// project .security.yaml, and the skill runtime. The cwd parameter is the
// project root (used for loading .security.yaml files). The rt parameter
// may be nil when no skill runtime is configured.
func buildPipeline(registry *tools.Registry, cfg *config.Config, cwd string, rt *skills.Runtime) pipelineComponents {
	classifier := toolexec.NewClassifier(nil)

	// Collect permission rules from all sources.
	var tomlConfs []toolexec.ToolRuleConf
	for _, r := range cfg.Agent.ToolRules {
		tomlConfs = append(tomlConfs, toolexec.ToolRuleConf{
			Category: r.Category,
			Tool:     r.Tool,
			Pattern:  r.Pattern,
			Action:   r.Action,
		})
	}
	userRules := toolexec.TOMLRulesToPermissionRules(tomlConfs, toolexec.SourceUser)

	// Load project .security.yaml rules.
	var projectRules []toolexec.PermissionRule
	if cwd != "" {
		projRules, _ := toolexec.LoadSecurityYAMLRules(filepath.Join(cwd, ".security.yaml"))
		projectRules = append(projectRules, projRules...)

		// Load local overrides (gitignored).
		localRules, _ := toolexec.LoadSecurityYAMLRules(filepath.Join(cwd, ".security.local.yaml"))
		for i := range localRules {
			localRules[i].Source = toolexec.SourceLocal
		}
		projectRules = append(projectRules, localRules...)
	}

	allRules := toolexec.MergeRules(userRules, projectRules)
	ruleEngine := toolexec.NewRuleEngine(allRules)
	shellValidator := toolexec.NewShellValidator(ruleEngine, cwd)
	hookAdapter := &toolexec.SkillHookAdapter{Runtime: rt}

	p := toolexec.NewPipeline(
		toolexec.RegistryExecutor(registry),
		toolexec.ClassifierMiddleware(classifier),
		toolexec.RuleEngineMiddleware(ruleEngine),
		toolexec.HookMiddleware(hookAdapter),
		toolexec.ShellSafetyMiddleware(shellValidator),
		toolexec.PostHookMiddleware(hookAdapter),
		toolexec.OutputManagerMiddleware(&toolexec.ResultStoreAdapter{Offloader: nil}),
	)
	return pipelineComponents{Pipeline: p, Classifier: classifier, RuleEngine: ruleEngine}
}

// ruleEngineChecker adapts the toolexec.RuleEngine to the agent.ApprovalChecker
// interface. ActionAllow maps to TrustRuleApproved (auto-approve); all other
// actions map to ApprovalRequired (deny is already handled by the pipeline
// middleware before approval is consulted).
type ruleEngineChecker struct {
	classifier *toolexec.Classifier
	engine     *toolexec.RuleEngine
}

func (c *ruleEngineChecker) CheckApproval(tool string, input json.RawMessage) agent.ApprovalResult {
	cat := c.classifier.Classify(tool)
	action := c.engine.Evaluate(cat, tool, input)
	if action == toolexec.ActionAllow {
		return agent.TrustRuleApproved
	}
	return agent.ApprovalRequired
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

	// Set up effective working directory (creates worktree if --worktree is set).
	cwd, wtMgr, wtCleanup, err := setupWorkingDir(cfg)
	if err != nil {
		return fmt.Errorf("worktree setup: %w", err)
	}
	defer wtCleanup()

	// Create tool registry
	registry := tools.NewRegistry()
	diffTracker := tools.NewDiffTracker()
	allowed := parseToolsFlag(toolsFlag)
	toolsCfg := ToolsConfig{
		ModelCapabilities: detectModelCapabilities(cfg),
		ProjectContext: ProjectContext{
			AppleProjectDetected: xcode.DiscoverProject(cwd).Type != "none",
			AppleSkillRequested:  containsSkill("apple-dev", skillsFlag),
		},
		CLIOverrides: allowed,
	}

	if toolsCfg.ShouldEnable("file") {
		fileTool := tools.NewFileTool(cwd)
		fileTool.SetDiffTracker(diffTracker)
		if err := registry.Register(fileTool); err != nil {
			return fmt.Errorf("registering file tool: %w", err)
		}
	}

	if toolsCfg.ShouldEnable("shell") {
		shellTool := tools.NewShellTool(cwd, 120*time.Second)
		shellTool.SetDiffTracker(diffTracker)
		if err := registry.Register(shellTool); err != nil {
			return fmt.Errorf("registering shell tool: %w", err)
		}
	}
	if toolsCfg.ShouldEnable("search") {
		if err := registry.Register(tools.NewSearchTool(cwd)); err != nil {
			return fmt.Errorf("registering search tool: %w", err)
		}
	}
	if toolsCfg.ShouldEnable("process") {
		procMgr := tools.NewProcessManager(cwd, tools.ProcessManagerConfig{})
		defer func() { _ = procMgr.Shutdown(context.Background()) }()
		if err := registry.Register(tools.NewProcessTool(procMgr)); err != nil {
			return fmt.Errorf("registering process tool: %w", err)
		}
	}

	// Auto-activate apple-dev Xcode tools if Apple project detected.
	var opts []agent.AgentOption
	opts = append(opts, agent.WithDiffTracker(diffTracker))
	if worktreeFlag != "" {
		opts = append(opts, agent.WithWorkingDir(cwd))
	}
	if err := wireAppleDev(cwd, registry, toolsCfg); err != nil {
		return err
	}
	// Prevent createSkillRuntime from trying to discover apple-dev again.
	skillsFlag = removeSkill("apple-dev", skillsFlag)

	// Wire wiki skill (generate_wiki tool).
	llmCompleter := integrations.NewLLMCompleter(p, cfg.Provider.Model)
	if err := wireWiki(cwd, registry, llmCompleter, toolsCfg); err != nil {
		return err
	}

	// --- Subagent system wiring ---

	// Create agent definition registry with built-in "general" definition.
	agentDefReg := agent.NewAgentDefRegistry()
	_ = agentDefReg.Register(&agent.AgentDef{
		Name:        "general",
		Description: "General-purpose agent with all available tools",
	})
	// Register config-defined agent definitions.
	for _, defConf := range cfg.Agent.Definitions {
		_ = agentDefReg.Register(&agent.AgentDef{
			Name:          defConf.Name,
			Description:   defConf.Description,
			SystemPrompt:  defConf.SystemPrompt,
			Tools:         defConf.Tools,
			MaxTurns:      defConf.MaxTurns,
			MaxDepth:      defConf.MaxDepth,
			Model:         defConf.Model,
			InheritSkills: defConf.InheritSkills,
			ExtraSkills:   defConf.ExtraSkills,
			DisableSkills: defConf.DisableSkills,
		})
	}

	// Create wake manager for background subagent notifications.
	wakeManager := agent.NewWakeManager()

	// Create spawner (provider will be set after agent creation).
	spawner := &agent.DefaultSubagentSpawner{
		Config:    cfg,
		AgentDefs: agentDefReg,
	}

	// Wire worktree provider for subagent isolation.
	// Always create a manager so subagents can use isolation: "worktree"
	// even when the parent isn't running in a worktree.
	if wtMgr != nil {
		spawner.WorktreeProvider = &worktreeProviderAdapter{mgr: wtMgr}
	} else if out, gitErr := runGitCommand("rev-parse", "--show-toplevel"); gitErr == nil {
		root := strings.TrimSpace(out)
		wtCfg := worktree.Config{
			MaxWorktrees: cfg.Worktree.MaxCount,
			BaseBranch:   cfg.Worktree.BaseBranch,
			AutoCleanup:  cfg.Worktree.AutoCleanup,
		}
		spawner.WorktreeProvider = &worktreeProviderAdapter{mgr: worktree.NewManager(root, wtCfg)}
	}

	// Register task and list_tasks tools.
	taskTool := tools.NewTaskTool(
		&spawnerAdapter{spawner: spawner},
		&agentDefLookupAdapter{reg: agentDefReg},
		0,
	)
	taskTool.SetBackgroundManager(&wakeManagerAdapter{wm: wakeManager})
	if toolsCfg.ShouldEnable("task") {
		if err := registry.Register(taskTool); err != nil {
			return fmt.Errorf("registering task tool: %w", err)
		}
	}
	if toolsCfg.ShouldEnable("list_tasks") {
		if err := registry.Register(tools.NewListTasksTool(&wakeStatusAdapter{wm: wakeManager})); err != nil {
			return fmt.Errorf("registering list_tasks tool: %w", err)
		}
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
	if err := ensureFolderAccessApproved(s, cwd, os.Stdin, os.Stderr); err != nil {
		return err
	}
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

	// Create skill runtime with built-in prompt skills and any explicit --skills.
	rt, storeCloser, err := createSkillRuntime(context.Background(), registry, p, cfg, "interactive", cwd)
	if err != nil {
		return fmt.Errorf("creating skill runtime: %w", err)
	}
	if storeCloser != nil {
		defer storeCloser.Close()
	}
	if rt != nil {
		emitSkillDiscoveryWarnings(os.Stderr, rt)
		opts = append(opts, agent.WithSkillRuntime(rt))
	}

	// Wire cross-session memory and summarizer.
	summarizer := agent.NewLLMSummarizer(p, cfg.Provider.Model)
	opts = append(opts, agent.WithSummarizer(summarizer))
	opts = append(opts, agent.WithMemoryStore(&storeMemoryAdapter{store: s}))

	// Create command registry and register built-in slash commands.
	// Built-in registration failures indicate a programming bug (duplicate names).
	cmdRegistry := commands.NewRegistry()
	for _, cmd := range []commands.SlashCommand{
		commands.NewQuitCommand(),
		commands.NewExitCommand(),
		commands.NewConfigCommand(),
		commands.NewHelpCommand(cmdRegistry),
		commands.NewInitCommand(cwd),
	} {
		if err := cmdRegistry.Register(cmd); err != nil {
			return fmt.Errorf("register built-in command %q: %w", cmd.Name(), err)
		}
	}

	// Wire command registry into skill runtime so skill-contributed commands
	// are registered on activation and unregistered on deactivation.
	if rt != nil {
		rt.SetCommandRegistry(cmdRegistry)
		rt.SetAgentDefRegistrar(&agentDefRegistrarAdapter{reg: agentDefReg})
	}

	// Create TUI model first (with nil agent) so we can extract the
	// interactive approval function before constructing the agent.
	model := tui.NewModel(nil, "rubichan", cfg.Provider.Model, cfg.Agent.MaxTurns, cfgPath, cfg, cmdRegistry)

	// Set git branch in status bar if available.
	if branch, err := detectGitBranch(cwd); err == nil && branch != "" {
		model.SetGitBranch(branch)
	}

	// Register commands that need model callbacks (these need the model instance).
	if err := cmdRegistry.Register(commands.NewClearCommand(func() {
		if model.GetAgent() != nil {
			model.GetAgent().ClearConversation()
		}
		model.ClearContent()
	})); err != nil {
		return fmt.Errorf("register built-in command %q: %w", "clear", err)
	}
	if err := cmdRegistry.Register(commands.NewRalphLoopCommand(model.StartRalphLoop)); err != nil {
		return fmt.Errorf("register built-in command %q: %w", "ralph-loop", err)
	}
	if err := cmdRegistry.Register(commands.NewCancelRalphCommand(model.CancelRalphLoop)); err != nil {
		return fmt.Errorf("register built-in command %q: %w", "cancel-ralph", err)
	}
	if err := cmdRegistry.Register(commands.NewModelCommand(func(name string) {
		if model.GetAgent() != nil {
			model.GetAgent().SetModel(name)
		}
		model.SwitchModel(name)
	})); err != nil {
		return fmt.Errorf("register built-in command %q: %w", "model", err)
	}
	if rt != nil {
		if err := cmdRegistry.Register(commands.NewSkillCommand(&skillListerAdapter{rt: rt})); err != nil {
			return fmt.Errorf("register built-in command %q: %w", "skill", err)
		}
	}

	// Build tool execution pipeline first so its rule engine can feed
	// the approval system.
	pc := buildPipeline(registry, cfg, cwd, rt)
	opts = append(opts, agent.WithPipeline(pc.Pipeline))

	if !autoApprove {
		approvalFunc = model.MakeApprovalFunc()

		// Build the approval checker: compose session cache, pipeline rule
		// engine, and config-based trust rules. Session cache (TUI "always"
		// decisions) is checked first, then the pipeline's rule engine
		// (category-based allow rules), then config trust rules.
		var checkers []agent.ApprovalChecker
		checkers = append(checkers, model) // session cache
		checkers = append(checkers, &ruleEngineChecker{
			classifier: pc.Classifier,
			engine:     pc.RuleEngine,
		})
		if len(cfg.Agent.TrustRules) > 0 {
			regexRules, globRules := splitTrustRules(cfg.Agent.TrustRules)
			if err := agent.ValidateTrustRules(regexRules, globRules); err != nil {
				return fmt.Errorf("invalid trust rules in config: %w", err)
			}
			checkers = append(checkers, agent.NewTrustRuleChecker(regexRules, globRules))
		}
		composite := agent.NewCompositeApprovalChecker(checkers...)
		opts = append(opts, agent.WithApprovalChecker(composite))
		spawner.ApprovalChecker = composite
	} else {
		opts = append(opts, agent.WithApprovalChecker(agent.AlwaysAutoApprove{}))
		spawner.ApprovalChecker = agent.AlwaysAutoApprove{}
	}

	// Attach the wake manager for background subagent notifications.
	opts = append(opts, agent.WithWakeManager(wakeManager))

	// Create agent with the approval function.
	a := agent.New(p, registry, approvalFunc, cfg, opts...)

	// Wire spawner dependencies that need the agent and provider.
	spawner.Provider = p
	spawner.ParentTools = registry
	spawner.ParentSkillRuntime = rt

	// Register notes tool backed by agent's scratchpad.
	if toolsCfg.ShouldEnable("notes") {
		if err := registry.Register(tools.NewNotesTool(a.ScratchpadAccess())); err != nil {
			return fmt.Errorf("registering notes tool: %w", err)
		}
	}

	// Wire the agent into the TUI model now that both exist.
	model.SetAgent(a)
	model.SetWikiConfig(tui.WikiCommandConfig{
		WorkDir: cwd,
		LLM:     llmCompleter,
	})

	// Index project files for @ mention autocomplete (background).
	fileSrc := tui.NewFileCompletionSource(cwd)
	model.SetFileCompletionSource(fileSrc)
	go indexProjectFiles(cwd, fileSrc)

	prog := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
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

	// Set up effective working directory (creates worktree if --worktree is set).
	cwd, _, wtCleanup, err := setupWorkingDir(cfg)
	if err != nil {
		return fmt.Errorf("worktree setup: %w", err)
	}
	defer wtCleanup()

	// Resolve input: code-review mode builds prompt from diff, others need explicit input.
	var promptText string
	if modeFlag == "code-review" {
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

	registry := tools.NewRegistry()
	allowed := parseToolsFlag(toolsFlag)
	headlessDiffTracker := tools.NewDiffTracker()
	headlessToolsCfg := ToolsConfig{
		ModelCapabilities: detectModelCapabilities(cfg),
		ProjectContext: ProjectContext{
			AppleProjectDetected: xcode.DiscoverProject(cwd).Type != "none",
			AppleSkillRequested:  containsSkill("apple-dev", skillsFlag),
		},
		CLIOverrides: allowed,
		HeadlessMode: true,
	}

	if headlessToolsCfg.ShouldEnable("file") {
		headlessFileTool := tools.NewFileTool(cwd)
		headlessFileTool.SetDiffTracker(headlessDiffTracker)
		if err := registry.Register(headlessFileTool); err != nil {
			return fmt.Errorf("registering file tool: %w", err)
		}
	}
	if headlessToolsCfg.ShouldEnable("shell") {
		headlessShellTool := tools.NewShellTool(cwd, timeoutFlag)
		headlessShellTool.SetDiffTracker(headlessDiffTracker)
		if err := registry.Register(headlessShellTool); err != nil {
			return fmt.Errorf("registering shell tool: %w", err)
		}
	}
	if headlessToolsCfg.ShouldEnable("search") {
		if err := registry.Register(tools.NewSearchTool(cwd)); err != nil {
			return fmt.Errorf("registering search tool: %w", err)
		}
	}
	if headlessToolsCfg.ShouldEnable("process") {
		pm := tools.NewProcessManager(cwd, tools.ProcessManagerConfig{})
		defer func() { _ = pm.Shutdown(context.Background()) }()
		if err := registry.Register(tools.NewProcessTool(pm)); err != nil {
			return fmt.Errorf("registering process tool: %w", err)
		}
	}

	// Auto-activate apple-dev Xcode tools if Apple project detected.
	var opts []agent.AgentOption
	opts = append(opts, agent.WithDiffTracker(headlessDiffTracker))
	if worktreeFlag != "" {
		opts = append(opts, agent.WithWorkingDir(cwd))
	}
	if err := wireAppleDev(cwd, registry, headlessToolsCfg); err != nil {
		return err
	}
	skillsFlag = removeSkill("apple-dev", skillsFlag)

	// Wire wiki skill (generate_wiki tool).
	headlessLLM := integrations.NewLLMCompleter(p, cfg.Provider.Model)
	if err := wireWiki(cwd, registry, headlessLLM, headlessToolsCfg); err != nil {
		return err
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
	if err := ensureFolderAccessApprovedNonInteractive(s, cwd, autoApprove); err != nil {
		return err
	}
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

	// Create skill runtime with built-in skills.
	headlessMode := modeFlag
	if headlessMode == "" {
		headlessMode = "headless"
	}
	rt, storeCloser, err := createSkillRuntime(ctx, registry, p, cfg, headlessMode, cwd)
	if err != nil {
		return fmt.Errorf("creating skill runtime: %w", err)
	}
	if storeCloser != nil {
		defer storeCloser.Close()
	}
	if rt != nil {
		emitSkillDiscoveryWarnings(os.Stderr, rt)
		// Apply the same tool admission policy to skill-contributed tools
		// that is used for built-in tools, ensuring a unified policy path.
		rt.SetToolAdmissionFunc(headlessToolsCfg.ShouldEnable)
		opts = append(opts, agent.WithSkillRuntime(rt))
	}

	// Wire cross-session memory and summarizer.
	headlessSummarizer := agent.NewLLMSummarizer(p, cfg.Provider.Model)
	opts = append(opts, agent.WithSummarizer(headlessSummarizer))
	opts = append(opts, agent.WithMemoryStore(&storeMemoryAdapter{store: s}))

	// Headless auto-approves all tools (restricted via --tools allowlist).
	// Parallelization is governed by a separate policy so the two concerns
	// are independent — a tool can be auto-approved but not parallelizable.
	opts = append(opts, agent.WithApprovalChecker(agent.AlwaysAutoApprove{}))
	opts = append(opts, agent.WithParallelPolicy(agent.AllowAllParallel{}))

	// Build tool execution pipeline.
	hpc := buildPipeline(registry, cfg, cwd, rt)
	opts = append(opts, agent.WithPipeline(hpc.Pipeline))

	a := agent.New(p, registry, approvalFunc, cfg, opts...)

	// Register notes tool backed by agent's scratchpad.
	if headlessToolsCfg.ShouldEnable("notes") {
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
		if term.IsTerminal(int(os.Stdout.Fd())) {
			width := 80
			if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
				width = w
			}
			formatter = output.NewStyledMarkdownFormatter(width)
		} else {
			formatter = output.NewMarkdownFormatter()
		}
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

	fmt.Fprintln(os.Stderr, persona.SuccessMessage())

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

// standardToolProfile lists the standard built-in tools included in the "all" profile.
var standardToolProfile = []string{"file", "shell", "search", "process", "notes"}

// parseToolsFlag splits a comma-separated tools string into a set.
// Returns nil if the input is empty (meaning no explicit allowlist).
// The special value "all" expands to the standard tool profile.
func parseToolsFlag(s string) map[string]bool {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	// Expand the "all" profile to standard tools.
	if strings.TrimSpace(s) == "all" {
		m := make(map[string]bool, len(standardToolProfile))
		for _, name := range standardToolProfile {
			m[name] = true
		}
		return m
	}
	m := make(map[string]bool)
	for _, t := range strings.Split(s, ",") {
		if name := strings.TrimSpace(t); name != "" {
			m[name] = true
		}
	}
	return m
}

// ModelCapabilities describes tool-related capabilities of the active model.
type ModelCapabilities struct {
	SupportsToolUse bool
}

// UserToolPrefs captures user-level tool preferences from config/runtime.
type UserToolPrefs struct {
	Enabled  map[string]bool
	Disabled map[string]bool
}

// ProjectContext captures project-derived tool gating signals.
type ProjectContext struct {
	AppleProjectDetected bool
	AppleSkillRequested  bool
}

// ToolsConfig centralizes tool enablement decisions.
//
// Decision order:
// 1) Model capability gate
// 2) Feature flags
// 3) User preferences
// 4) Project context constraints
// 5) CLI whitelist overrides
type ToolsConfig struct {
	ModelCapabilities ModelCapabilities
	UserPreferences   UserToolPrefs
	ProjectContext    ProjectContext
	FeatureFlags      map[string]bool
	CLIOverrides      map[string]bool
	// HeadlessMode enables default-deny behavior: when true and CLIOverrides
	// is nil, ShouldEnable returns false for all tools. This requires headless
	// callers to provide an explicit --tools allowlist or named profile.
	HeadlessMode bool
}

// detectModelCapabilities derives tool-related model capability flags from
// runtime config/provider metadata.
func detectModelCapabilities(cfg *config.Config) ModelCapabilities {
	if cfg == nil {
		return ModelCapabilities{SupportsToolUse: false}
	}

	switch cfg.Provider.Default {
	case "anthropic", "ollama":
		return ModelCapabilities{SupportsToolUse: true}
	default:
		// OpenAI-compatible providers are configured by name under
		// provider.openai_compatible. If the default provider matches one of
		// those configured entries, treat it as tool-capable.
		for _, oc := range cfg.Provider.OpenAI {
			if oc.Name == cfg.Provider.Default {
				return ModelCapabilities{SupportsToolUse: true}
			}
		}
		// Unknown provider type: treat as not tool-capable.
		return ModelCapabilities{SupportsToolUse: false}
	}
}

// ShouldEnable returns true if a tool should be registered.
func (tc ToolsConfig) ShouldEnable(name string) bool {
	if !tc.ModelCapabilities.SupportsToolUse {
		return false
	}

	if tc.FeatureFlags != nil {
		if enabled, ok := tc.FeatureFlags["tools."+name]; ok && !enabled {
			return false
		}
	}

	if tc.UserPreferences.Disabled != nil && tc.UserPreferences.Disabled[name] {
		return false
	}
	if len(tc.UserPreferences.Enabled) > 0 && !tc.UserPreferences.Enabled[name] {
		return false
	}

	// Apple-only tools are enabled only for Apple projects or explicit apple-dev opt-in.
	if isAppleOnlyTool(name) &&
		!tc.ProjectContext.AppleProjectDetected &&
		!tc.ProjectContext.AppleSkillRequested {
		return false
	}

	// CLI overrides act as an explicit whitelist.
	if tc.CLIOverrides != nil {
		return tc.CLIOverrides[name]
	}

	// Headless mode is default-deny: require an explicit --tools allowlist.
	if tc.HeadlessMode {
		return false
	}
	return true
}

func isAppleOnlyTool(name string) bool {
	return strings.HasPrefix(name, "xcode_") ||
		strings.HasPrefix(name, "swift_") ||
		strings.HasPrefix(name, "sim_") ||
		strings.HasPrefix(name, "codesign_") ||
		name == "xcrun" ||
		strings.HasPrefix(name, "xcrun_")
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

// wireAppleDev registers Xcode tools and delegates enablement decisions to ToolsConfig.
// The Apple system prompt is now registered as a built-in skill via
// appledev.RegisterPrompt and injected through the skill runtime's PromptCollector.
func wireAppleDev(cwd string, registry *tools.Registry, toolsCfg ToolsConfig) error {
	if !toolsCfg.ProjectContext.AppleProjectDetected && !toolsCfg.ProjectContext.AppleSkillRequested {
		return nil
	}

	pc := xcode.NewRealPlatformChecker()
	appleBackend := &appledev.Backend{WorkDir: cwd, Platform: pc}
	if err := appleBackend.Load(appledev.Manifest(), nil); err != nil {
		return fmt.Errorf("loading apple-dev skill: %w", err)
	}
	for _, tool := range appleBackend.Tools() {
		if toolsCfg.ShouldEnable(tool.Name()) {
			if err := registry.Register(tool); err != nil {
				return fmt.Errorf("registering xcode tool %s: %w", tool.Name(), err)
			}
		}
	}
	return nil
}

// wireWiki creates the wiki skill backend and registers its generate_wiki tool.
// The llmCompleter is injected from the caller (created from the provider).
func wireWiki(cwd string, registry *tools.Registry, llm wiki.LLMCompleter, toolsCfg ToolsConfig) error {
	backend := &builtin.WikiBackend{WorkDir: cwd, LLM: llm}
	if err := backend.Load(builtin.WikiManifest(), nil); err != nil {
		return fmt.Errorf("loading wiki skill: %w", err)
	}
	for _, tool := range backend.Tools() {
		if toolsCfg.ShouldEnable(tool.Name()) {
			if err := registry.Register(tool); err != nil {
				return fmt.Errorf("registering wiki tool %s: %w", tool.Name(), err)
			}
		}
	}
	return nil
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

// splitTrustRules separates config trust rules into regex and glob rule slices.
// Rules with a Glob field are treated as glob rules; all others as regex rules.
func splitTrustRules(rules []config.TrustRuleConf) ([]agent.TrustRule, []agent.GlobTrustRule) {
	var regex []agent.TrustRule
	var globs []agent.GlobTrustRule
	for _, r := range rules {
		if r.Glob != "" {
			globs = append(globs, agent.GlobTrustRule{Glob: r.Glob, Action: r.Action})
		} else {
			regex = append(regex, agent.TrustRule{Tool: r.Tool, Pattern: r.Pattern, Action: r.Action})
		}
	}
	return regex, globs
}

// --- Subagent system adapter types ---
//
// These adapters bridge the agent package (which has the real implementations)
// to the local interfaces defined in the tools/ and skills/ packages, converting
// between type-specific config/result structs to avoid import cycles.

// spawnerAdapter bridges agent.DefaultSubagentSpawner to the tools.TaskSpawner
// interface, converting between type-specific config/result structs.
type spawnerAdapter struct {
	spawner *agent.DefaultSubagentSpawner
}

func (a *spawnerAdapter) Spawn(ctx context.Context, cfg tools.TaskSpawnConfig, prompt string) (*tools.TaskSpawnResult, error) {
	result, err := a.spawner.Spawn(ctx, agent.SubagentConfig{
		Name:          cfg.Name,
		SystemPrompt:  cfg.SystemPrompt,
		Tools:         cfg.Tools,
		MaxTurns:      cfg.MaxTurns,
		MaxTokens:     cfg.MaxTokens,
		Model:         cfg.Model,
		Depth:         cfg.Depth,
		MaxDepth:      cfg.MaxDepth,
		InheritSkills: cfg.InheritSkills,
		ExtraSkills:   cfg.ExtraSkills,
		DisableSkills: cfg.DisableSkills,
	}, prompt)
	if err != nil {
		return nil, err
	}
	return &tools.TaskSpawnResult{
		Name:         result.Name,
		Output:       result.Output,
		ToolsUsed:    result.ToolsUsed,
		TurnCount:    result.TurnCount,
		InputTokens:  result.InputTokens,
		OutputTokens: result.OutputTokens,
		Error:        result.Error,
	}, nil
}

// agentDefLookupAdapter bridges agent.AgentDefRegistry to tools.TaskAgentDefLookup.
type agentDefLookupAdapter struct {
	reg *agent.AgentDefRegistry
}

func (a *agentDefLookupAdapter) GetAgentDef(name string) (*tools.TaskAgentDef, bool) {
	def, ok := a.reg.Get(name)
	if !ok {
		return nil, false
	}
	return &tools.TaskAgentDef{
		Name:          def.Name,
		SystemPrompt:  def.SystemPrompt,
		Tools:         def.Tools,
		MaxTurns:      def.MaxTurns,
		MaxDepth:      def.MaxDepth,
		Model:         def.Model,
		InheritSkills: def.InheritSkills,
		ExtraSkills:   def.ExtraSkills,
		DisableSkills: def.DisableSkills,
	}, true
}

// wakeManagerAdapter bridges agent.WakeManager to tools.BackgroundTaskManager.
type wakeManagerAdapter struct {
	wm *agent.WakeManager
}

func (a *wakeManagerAdapter) SubmitBackground(name string, cancel context.CancelFunc) string {
	return a.wm.Submit(name, cancel)
}

func (a *wakeManagerAdapter) CompleteBackground(taskID string, output string, err error) {
	a.wm.Complete(taskID, &agent.SubagentResult{
		Output: output,
		Error:  err,
	})
}

// wakeStatusAdapter bridges agent.WakeManager to tools.TaskStatusProvider.
type wakeStatusAdapter struct {
	wm *agent.WakeManager
}

func (a *wakeStatusAdapter) BackgroundTaskStatus() []tools.BackgroundTaskInfo {
	statuses := a.wm.Status()
	result := make([]tools.BackgroundTaskInfo, len(statuses))
	for i, s := range statuses {
		result[i] = tools.BackgroundTaskInfo{
			ID:        s.ID,
			AgentName: s.AgentName,
			Status:    s.Status,
		}
	}
	return result
}

// agentDefRegistrarAdapter bridges agent.AgentDefRegistry to skills.AgentDefRegistrar.
type agentDefRegistrarAdapter struct {
	reg *agent.AgentDefRegistry
}

func (a *agentDefRegistrarAdapter) Register(def *skills.AgentDefinition) error {
	return a.reg.Register(&agent.AgentDef{
		Name:          def.Name,
		Description:   def.Description,
		SystemPrompt:  def.SystemPrompt,
		Tools:         def.Tools,
		MaxTurns:      def.MaxTurns,
		MaxDepth:      def.MaxDepth,
		Model:         def.Model,
		InheritSkills: def.InheritSkills,
		ExtraSkills:   def.ExtraSkills,
		DisableSkills: def.DisableSkills,
	})
}

func (a *agentDefRegistrarAdapter) Unregister(name string) error {
	return a.reg.Unregister(name)
}

// skillListerAdapter bridges skills.Runtime to commands.SkillLister.
type skillListerAdapter struct {
	rt *skills.Runtime
}

func (a *skillListerAdapter) ListSkills() []commands.SkillInfo {
	summaries := a.rt.GetAllSkillSummaries()
	infos := make([]commands.SkillInfo, len(summaries))
	for i, s := range summaries {
		infos[i] = commands.SkillInfo{
			Name:        s.Name,
			Description: s.Description,
			Source:      string(s.Source),
			State:       s.State.String(),
		}
	}
	return infos
}

func (a *skillListerAdapter) ActivateSkill(name string) error {
	return a.rt.Activate(name)
}

func (a *skillListerAdapter) DeactivateSkill(name string) error {
	return a.rt.Deactivate(name)
}

// indexProjectFiles populates the file completion source from git ls-files
// (or falls back to nothing for non-git directories).
func indexProjectFiles(dir string, src *tui.FileCompletionSource) {
	cmd := exec.Command("git", "-C", dir, "ls-files")
	out, err := cmd.Output()
	if err != nil {
		return // not a git repo or git not installed — skip silently
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return
	}
	src.SetFiles(lines)
}

// detectGitBranch returns the current git branch name, or an error if
// the directory is not a git repository.
func detectGitBranch(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	branch := strings.TrimSpace(string(out))
	// In detached HEAD state, git returns literal "HEAD" — filter it out.
	if branch == "HEAD" {
		return "", nil
	}
	return branch, nil
}
