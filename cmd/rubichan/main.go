// cmd/rubichan/main.go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"

	"github.com/sourcegraph/conc"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/checkpoint"
	"github.com/julianshen/rubichan/internal/cmux"
	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/diag"
	"github.com/julianshen/rubichan/internal/folderaccess"
	"github.com/julianshen/rubichan/internal/hooks"
	"github.com/julianshen/rubichan/internal/integrations"
	"github.com/julianshen/rubichan/internal/knowledgegraph"
	"github.com/julianshen/rubichan/internal/modelcheck"
	"github.com/julianshen/rubichan/internal/output"
	"github.com/julianshen/rubichan/internal/parser"
	"github.com/julianshen/rubichan/internal/permissions"
	"github.com/julianshen/rubichan/internal/persona"
	"github.com/julianshen/rubichan/internal/pipeline"
	"github.com/julianshen/rubichan/internal/platform"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/runner"
	"github.com/julianshen/rubichan/internal/security"
	"github.com/julianshen/rubichan/internal/security/analyzer"
	secoutput "github.com/julianshen/rubichan/internal/security/output"
	"github.com/julianshen/rubichan/internal/security/scanner"
	"github.com/julianshen/rubichan/internal/skillruntime"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/skills/builtin"
	"github.com/julianshen/rubichan/internal/skills/builtin/appledev"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/subagents"
	"github.com/julianshen/rubichan/internal/terminal"
	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/internal/tools/browser"
	dbtools "github.com/julianshen/rubichan/internal/tools/db"
	gittools "github.com/julianshen/rubichan/internal/tools/git"
	httptool "github.com/julianshen/rubichan/internal/tools/http"
	"github.com/julianshen/rubichan/internal/tools/lsp"
	toolsandbox "github.com/julianshen/rubichan/internal/tools/sandbox"
	"github.com/julianshen/rubichan/internal/tools/xcode"
	"github.com/julianshen/rubichan/internal/tui"
	"github.com/julianshen/rubichan/internal/wiki"
	"github.com/julianshen/rubichan/internal/worktree"
	"github.com/julianshen/rubichan/pkg/agentsdk"

	"golang.org/x/term"

	// Register providers via init() side effects.
	_ "github.com/julianshen/rubichan/internal/provider/anthropic"
	"github.com/julianshen/rubichan/internal/provider/ollama"
	_ "github.com/julianshen/rubichan/internal/provider/openai"
	_ "github.com/julianshen/rubichan/internal/provider/zai"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"

	configPath       string
	modelFlag        string
	providerFlag     string
	apiBaseFlag      string
	apiKeyFlag       string
	autoApprove      bool
	noColor          bool
	noAltScreen      bool
	noMouse          bool
	plainTUI         bool
	plainInteractive bool
	debugMode        bool
	testFlag         bool
	approveCwd       bool
	eventLogPath     string

	headless     bool
	acpFlag      bool
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
	forkFlag     bool
	failOnFlag   string
	worktreeFlag string

	postToPRFlag    bool
	prNumberFlag    int
	uploadSARIFFlag bool
	annotationsFlag bool

	wikiFlag            bool
	wikiOutFlag         string
	wikiFormatFlag      string
	wikiConcurrencyFlag int

	newProviderWithDebug = provider.NewProviderWithDebug
)

func versionString() string {
	return fmt.Sprintf("rubichan %s (commit: %s, built: %s)", version, commit, date)
}

// shouldIgnoreTUIRunError suppresses Bubble Tea's program-killed error when
// Rubichan intentionally cancelled the TUI context as part of signal handling.
func shouldIgnoreTUIRunError(err error, runCtx context.Context) bool {
	return errors.Is(err, tea.ErrProgramKilled) &&
		errors.Is(runCtx.Err(), context.Canceled) &&
		signalAbortFromContext(runCtx) == nil
}

type interactiveSignalAbort struct {
	name     string
	exitCode int
}

func (e *interactiveSignalAbort) Error() string {
	return fmt.Sprintf("interactive session aborted by %s", e.name)
}

func signalAbortFromContext(ctx context.Context) *interactiveSignalAbort {
	var abort *interactiveSignalAbort
	if errors.As(context.Cause(ctx), &abort) {
		return abort
	}
	return nil
}

func interactiveExitError(runCtx context.Context) error {
	if abort := signalAbortFromContext(runCtx); abort != nil {
		return &runner.ExitError{Code: abort.exitCode}
	}
	return nil
}

func appendWorkingDirOption(opts []agent.AgentOption, cwd string) []agent.AgentOption {
	if cwd == "" {
		return opts
	}
	return append(opts, agent.WithWorkingDir(cwd))
}

// wireSandboxProxy creates a DomainProxy if sandbox is enabled with allowed domains,
// configures the shell tool, and validates hard-lockdown requirements.
// Returns a cleanup function that must be deferred, or nil if no proxy was started.
func wireSandboxProxy(cfg *config.Config, shellTool *tools.ShellTool) (cleanup func(), err error) {
	var proxy *toolsandbox.DomainProxy
	if cfg.Sandbox.IsEnabled() && len(cfg.Sandbox.Network.AllowedDomains) > 0 {
		proxy = toolsandbox.NewDomainProxy(
			cfg.Sandbox.Network.AllowedDomains,
			toolsandbox.WithOnBlocked(func(domain, cmd string) {
				log.Printf("[sandbox] blocked connection to %s", domain)
			}),
		)
		if _, err := proxy.Start(); err != nil {
			log.Printf("warning: sandbox proxy failed to start: %v", err)
			proxy = nil
		}
	}
	shellTool.SetSandboxConfig(cfg.Sandbox, proxy)

	if cfg.Sandbox.IsEnabled() && !cfg.Sandbox.IsAllowUnsandboxedCommands() && shellTool.Sandbox() == nil {
		if proxy != nil {
			_ = proxy.Stop()
		}
		return nil, fmt.Errorf("sandbox enabled with allow_unsandboxed_commands=false but no sandbox backend available")
	}

	if proxy != nil {
		return func() { _ = proxy.Stop() }, nil
	}
	return nil, nil
}

// wireLSPTools registers LSP tools into the registry if LSP is enabled.
// Returns a cleanup function that must be deferred, or nil if LSP is disabled.
// The cleanup function uses context.Background() because it runs during defers
// after the caller's context is already cancelled — it always needs its full timeout.
func wireLSPTools(cfg *config.Config, registry *tools.Registry, toolsCfg ToolsConfig, cwd string) (mgr *lsp.Manager, cleanup func(), err error) {
	if !cfg.LSP.IsEnabled() {
		return nil, nil, nil
	}
	lspRegistry := lsp.NewRegistry()
	lspManager := lsp.NewManager(lspRegistry, cwd, cfg.LSP.IsAutoInstall())
	for _, tool := range lsp.AllTools(lspManager) {
		if toolsCfg.ShouldEnable(tool.Name()) {
			if err := registry.Register(tool); err != nil {
				return nil, nil, fmt.Errorf("registering LSP tool %s: %w", tool.Name(), err)
			}
		}
	}
	return lspManager, func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := lspManager.Shutdown(shutCtx); err != nil {
			log.Printf("LSP shutdown: %v", err)
		}
	}, nil
}

func handleInteractiveProgramError(err error, runCtx context.Context, phase string) error {
	if exitErr := interactiveExitError(runCtx); exitErr != nil && errors.Is(err, tea.ErrProgramKilled) {
		return exitErr
	}
	if err == nil || shouldIgnoreTUIRunError(err, runCtx) {
		return nil
	}
	return fmt.Errorf("%s: %w", phase, err)
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

	root, gitErr := gitRepoRoot()
	if gitErr != nil {
		return "", nil, nil, fmt.Errorf("not in a git repository: %w", gitErr)
	}

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

// gitRepoRoot returns the repository root, for callers that need one without
// caring how it is found.
func gitRepoRoot() (string, error) {
	out, err := runGitCommand("rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("resolving git repository root: %w", err)
	}
	return strings.TrimSpace(out), nil
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
	cfgDir, cfgDirErr := configDir()
	defer func() {
		if r := recover(); r != nil {
			sessionLogPath := diag.ActiveSessionLogPath()
			if cfgDirErr == nil {
				if path, err := diag.WritePanicDump(cfgDir, r, sessionLogPath); err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to write panic dump: %v\n", err)
				} else {
					fmt.Fprintf(os.Stderr, "panic dump written to %s\n", path)
				}
			} else {
				fmt.Fprintf(os.Stderr, "warning: failed to resolve config directory for panic dump: %v\n", cfgDirErr)
			}
			panic(r)
		}
	}()

	rootCmd := &cobra.Command{
		Use:   "rubichan",
		Short: "An AI coding assistant",
		Long:  "rubichan — an interactive AI coding assistant powered by LLMs.",
		RunE: func(_ *cobra.Command, _ []string) error {
			if testFlag {
				return runModelCapabilityTest()
			}
			if wikiFlag {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("getting working directory: %w", err)
				}
				cfg, err := loadConfig()
				if err != nil {
					return err
				}
				return runWikiHeadless(cfg, cwd, wikiOutFlag, wikiFormatFlag, wikiConcurrencyFlag)
			}
			if acpFlag {
				return runACP()
			}
			if headless {
				return runHeadless()
			}
			return runInteractive()
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			// Suppress ANSI color globally when --no-color is set or the
			// NO_COLOR environment variable is present (https://no-color.org/).
			if noColor || os.Getenv("NO_COLOR") != "" {
				lipgloss.SetColorProfile(termenv.Ascii)
			}
		},
	}

	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "path to config file")
	rootCmd.PersistentFlags().StringVar(&modelFlag, "model", "", "override model name")
	rootCmd.PersistentFlags().StringVar(&providerFlag, "provider", "", "override provider name")
	rootCmd.PersistentFlags().StringVar(&apiBaseFlag, "api-base", "", "base URL for OpenAI-compatible API (e.g. http://localhost:1234/v1)")
	rootCmd.PersistentFlags().StringVar(&apiKeyFlag, "api-key", "", "API key for the provider (use 'none' for local servers)")
	rootCmd.PersistentFlags().BoolVar(&autoApprove, "auto-approve", false, "auto-approve all tool calls (dangerous: enables RCE)")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "suppress all ANSI color output (also respects NO_COLOR env var)")
	rootCmd.PersistentFlags().BoolVar(&noAltScreen, "no-alt-screen", false, "run interactive TUI in the normal terminal buffer")
	rootCmd.PersistentFlags().BoolVar(&noMouse, "no-mouse", false, "disable mouse tracking in the interactive TUI")
	rootCmd.PersistentFlags().BoolVar(&plainTUI, "plain-tui", false, "run a reduced interactive TUI optimized for PTY capture and automation")
	rootCmd.PersistentFlags().BoolVar(&plainInteractive, "plain-interactive", false, "run a non-TUI interactive REPL that shares the interactive agent, skills, and approval flow")
	rootCmd.PersistentFlags().BoolVar(&debugMode, "debug", false, "enable interactive debug surfaces and mirror logs to stderr")
	rootCmd.PersistentFlags().BoolVar(&testFlag, "test", false, "test configured provider/model capabilities and connectivity")
	rootCmd.PersistentFlags().BoolVar(&approveCwd, "approve-cwd", false, "approve access to the current working directory in headless mode without enabling full auto-approval")
	rootCmd.PersistentFlags().StringVar(&eventLogPath, "event-log", "", "write structured interactive session events to the given JSONL file")
	rootCmd.PersistentFlags().BoolVar(&headless, "headless", false, "run in non-interactive headless mode")
	rootCmd.PersistentFlags().BoolVar(&acpFlag, "acp", false, "serve the Agent Client Protocol over stdin/stdout for an editor client")
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
	rootCmd.PersistentFlags().StringVarP(&resumeFlag, "resume", "r", "", "resume a previous session by ID")
	rootCmd.PersistentFlags().BoolVar(&forkFlag, "fork", false, "fork the resumed session instead of continuing it")
	rootCmd.PersistentFlags().StringVar(&failOnFlag, "fail-on", "", "exit non-zero if findings at/above severity (critical, high, medium, low)")
	rootCmd.PersistentFlags().StringVar(&worktreeFlag, "worktree", "", "run in an isolated git worktree with the given name")
	rootCmd.PersistentFlags().BoolVar(&postToPRFlag, "post-to-pr", false, "post results as a PR/MR comment (auto-detects GitHub/GitLab)")
	rootCmd.PersistentFlags().IntVar(&prNumberFlag, "pr", 0, "explicit PR/MR number (overrides auto-detection)")
	rootCmd.PersistentFlags().BoolVar(&uploadSARIFFlag, "upload-sarif", false, "upload SARIF to GitHub Code Scanning")
	rootCmd.PersistentFlags().BoolVar(&annotationsFlag, "annotations", false, "emit GitHub Actions workflow annotations")
	rootCmd.PersistentFlags().BoolVar(&wikiFlag, "wiki", false, "run wiki generation (implies --headless, --approve-cwd)")
	rootCmd.PersistentFlags().StringVar(&wikiOutFlag, "wiki-out", "docs/wiki", "output directory for wiki files")
	rootCmd.PersistentFlags().StringVar(&wikiFormatFlag, "wiki-format", "raw-md", "wiki output format: raw-md, hugo, docusaurus")
	rootCmd.PersistentFlags().IntVar(&wikiConcurrencyFlag, "wiki-concurrency", 5, "max parallel LLM calls for wiki generation")

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println(versionString())
		},
	}

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(replayCmd())
	rootCmd.AddCommand(skillCmd())
	// wiki is now a built-in skill (generate_wiki tool), not a CLI subcommand.
	rootCmd.AddCommand(ollamaCmd())
	rootCmd.AddCommand(knowledgeCmd())
	rootCmd.AddCommand(initKnowledgeGraphCmd())
	rootCmd.AddCommand(worktreeCmd())
	rootCmd.AddCommand(sessionCmd())
	rootCmd.AddCommand(initCmd())
	rootCmd.AddCommand(serveCmd())
	rootCmd.AddCommand(shellCmd())

	if err := rootCmd.Execute(); err != nil {
		var exitErr *runner.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		fmt.Fprint(os.Stderr, persona.ErrorMessage(err.Error()))
		os.Exit(1)
	}
}

func runModelCapabilityTest() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	p, err := newProviderWithDebug(cfg, debugMode)
	if err != nil {
		return fmt.Errorf("creating provider: %w", err)
	}

	return modelcheck.Run(context.Background(), os.Stdout, p, cfg.Provider.Default, cfg.Provider.Model)
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

// createSkillRuntime builds the skill runtime for a mode, resolving the CLI's
// skill flags into the selection the runtime package expects. The config
// directory comes from the caller, which already resolved it.
func createSkillRuntime(ctx context.Context, registry *tools.Registry, p provider.LLMProvider, cfg *config.Config, mode, workDir, cfgDir string) (*skills.Runtime, io.Closer, error) {
	return skillruntime.New(ctx, skillruntime.Options{
		Registry:    registry,
		Provider:    p,
		Config:      cfg,
		Mode:        mode,
		WorkDir:     workDir,
		ConfigDir:   cfgDir,
		SkillNames:  parseSkillsFlag(skillsFlag),
		AutoApprove: approveSkillsFlag,
	})
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

func appendPersonaOptions(opts []agent.AgentOption, cwd string) []agent.AgentOption {
	type loader struct {
		name string
		fn   func(string) (string, error)
		opt  func(string) agent.AgentOption
	}

	loaders := []loader{
		{name: "AGENT.md", fn: config.LoadAgentMD, opt: agent.WithAgentMD},
		{name: "IDENTITY.md", fn: config.LoadIdentityMD, opt: agent.WithIdentityMD},
		{name: "SOUL.md", fn: config.LoadSoulMD, opt: agent.WithSoulMD},
	}

	for _, l := range loaders {
		content, err := l.fn(cwd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to load %s: %v\n", l.name, err)
			continue
		}
		if content == "" {
			continue
		}
		fmt.Fprintf(os.Stderr, "warning: loading trusted workspace prompt file %s into the system prompt; review it before using Rubichan on untrusted repositories\n", l.name)
		opts = append(opts, l.opt(content))
	}

	return opts
}

// appendKnowledgeGraphOption adds knowledge graph integration to the agent options.
// It gracefully degrades if the knowledge graph cannot be opened (no warnings logged).
func appendKnowledgeGraphOption(ctx context.Context, opts []agent.AgentOption, workDir string) []agent.AgentOption {
	g, err := openGraph(ctx, workDir)
	if err != nil {
		// Graceful degradation: knowledge graph is optional
		return opts
	}

	// Cast to internal KnowledgeGraph type to access NewContextSelector
	kg, ok := g.(*knowledgegraph.KnowledgeGraph)
	if !ok {
		// Graceful degradation if cast fails
		return opts
	}

	selector := knowledgegraph.NewContextSelector(kg)
	return append(opts, agent.WithKnowledgeGraph(selector))
}

// convertHookRules converts config hook rules into UserHookConfig entries.
func convertHookRules(rules []config.HookRuleConfig, source string) []hooks.UserHookConfig {
	var out []hooks.UserHookConfig
	for _, rule := range rules {
		out = append(out, hooks.UserHookConfig{
			Event: rule.Event, Pattern: rule.Pattern, Command: rule.Command,
			Description: rule.Description, Timeout: hooks.ParseHookTimeout(rule.Timeout), Source: source,
		})
	}
	return out
}

// loadProjectHooks builds the full list of user hook configs from config rules,
// .agent/hooks.toml, and AGENT.md frontmatter. Project-level hooks (TOML and
// AGENT.md) are gated by trust approval.
func loadProjectHooks(cfg *config.Config, s hooks.HookApprovalStore, cwd string) []hooks.UserHookConfig {
	configs := convertHookRules(cfg.Hooks.Rules, "config")

	tomlHooks, tomlErr := hooks.LoadHooksTOML(cwd)
	if tomlErr != nil {
		log.Printf("warning: loading .agent/hooks.toml: %v", tomlErr)
	}
	if len(tomlHooks) > 0 {
		if cfg.Hooks.TrustProjectHooks {
			configs = append(configs, tomlHooks...)
		} else if trusted, _ := hooks.CheckTrust(s, cwd, tomlHooks); trusted {
			configs = append(configs, tomlHooks...)
		} else {
			log.Printf("Project hooks in .agent/hooks.toml not trusted — skipping.")
		}
	}

	_, agentMDHooks, _ := config.LoadAgentMDWithHooks(cwd)
	if len(agentMDHooks) > 0 {
		projectHooks := convertHookRules(agentMDHooks, "agent.md")
		if cfg.Hooks.TrustProjectHooks {
			configs = append(configs, projectHooks...)
		} else if trusted, _ := hooks.CheckTrust(s, cwd, projectHooks); trusted {
			configs = append(configs, projectHooks...)
		} else if len(projectHooks) > 0 {
			log.Printf("Project hooks in AGENT.md not trusted — skipping.")
		}
	}

	return configs
}

const memorySaveTimeout = 5 * time.Second

func saveMemoriesBestEffort(parentCtx context.Context, a *agent.Agent, out io.Writer) {
	if a == nil {
		return
	}

	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(parentCtx, memorySaveTimeout)
	defer cancel()

	if err := a.SaveMemories(ctx); err != nil {
		fmt.Fprintf(out, "warning: failed to save memories: %v\n", err)
	}
}

// newDefaultSecurityEngine creates a security engine pre-configured with all
// built-in static scanners and, when an LLM provider is given, the phase-2
// LLM analyzers. This lives in main.go to avoid an import cycle between
// security/ and security/scanner/ (and security/analyzer/).
func newDefaultSecurityEngine(cfg security.EngineConfig, llm provider.LLMProvider) *security.Engine {
	e := security.NewEngine(cfg)
	e.AddScanner(scanner.NewSecretScanner())
	e.AddScanner(scanner.NewSASTScanner())
	e.AddScanner(scanner.NewConfigScanner())
	e.AddScanner(scanner.NewDepScanner(nil))
	e.AddScanner(scanner.NewLicenseScanner())
	e.AddScanner(scanner.NewAppleScanner())

	// Phase 2: LLM-powered deep analyzers (only when a provider is available).
	if llm != nil {
		e.AddAnalyzer(analyzer.NewAuthAnalyzer(llm))
		e.AddAnalyzer(analyzer.NewConcurrencyAnalyzer(llm))
		e.AddAnalyzer(analyzer.NewCryptoAnalyzer(llm))
		e.AddAnalyzer(analyzer.NewDataFlowAnalyzer(llm))
		e.AddAnalyzer(analyzer.NewBusinessLogicAnalyzer(llm))
	}
	return e
}

// securityScanCompleteHook returns an OnScanComplete callback that forwards
// the final scan report to the skill runtime as a HookOnSecurityScanComplete
// event so security-rule skills can observe scan results. The full Findings
// and AttackChains slices are exposed so handlers can react to specific
// vulnerabilities, not just summary counts.
func securityScanCompleteHook(rt *skills.Runtime) func(context.Context, *security.Report) {
	return func(ctx context.Context, report *security.Report) {
		if rt == nil || report == nil {
			return
		}
		if _, err := rt.DispatchHook(skills.HookEvent{
			Phase: skills.HookOnSecurityScanComplete,
			Ctx:   ctx,
			Data: map[string]any{
				skills.HookDataFindings:      report.Findings,
				skills.HookDataAttackChains:  report.AttackChains,
				skills.HookDataErrors:        report.Errors,
				skills.HookDataFindingsCount: report.Stats.FindingsCount,
				skills.HookDataChainCount:    report.Stats.ChainCount,
				skills.HookDataFilesScanned:  report.Stats.FilesScanned,
				skills.HookDataDurationMs:    report.Stats.Duration.Milliseconds(),
				skills.HookDataErrorsCount:   len(report.Errors),
			},
		}); err != nil {
			log.Printf("HookOnSecurityScanComplete failed: %v", err)
		}
	}
}

// pipelineComponents holds the composition root's slot middlewares and
// the key components for integration with the approval system. The agent
// owns the core pipeline chain (hooks, checkpoint, verdict, output) and
// splices these middlewares in via agent.WithToolMiddlewares.
type pipelineComponents struct {
	Middlewares agent.ToolMiddlewares
	Classifier  *toolexec.Classifier
	RuleEngine  *toolexec.RuleEngine
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

	// Hook, checkpoint, post-hook, verdict, and output middlewares are
	// composed by the agent itself — supplying them here would either
	// duplicate them or (as happened before this seam existed) silently
	// drop the ones needing agent state, leaving /undo without captures.
	mws := agent.ToolMiddlewares{
		BeforeHooks: []toolexec.Middleware{
			toolexec.SchemaValidationMiddleware(registry),
			toolexec.ClassifierMiddleware(classifier),
			toolexec.RuleEngineMiddleware(ruleEngine),
		},
		AfterHooks: []toolexec.Middleware{
			toolexec.ShellSafetyMiddleware(shellValidator),
		},
	}
	return pipelineComponents{Middlewares: mws, Classifier: classifier, RuleEngine: ruleEngine}
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

	// When --api-base is provided, ensure an OpenAI-compatible entry exists
	// for the current provider so users don't have to write TOML config for
	// simple local setups (e.g. rubichan --provider my-server --api-base http://localhost:1234/v1 --model coder).
	if apiBaseFlag != "" {
		applyAPIBaseFlag(cfg)
	} else if apiKeyFlag != "" {
		applyAPIKeyFlag(cfg)
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

	// Resolve the provider's default model if none was specified via
	// --model or config. Each provider's own resolution logic (constant,
	// config-driven fallback, or dynamic lookup) lives in its ProviderDef
	// (internal/provider/{anthropic,zai,ollama}). Providers with no
	// DefaultModel resolver (e.g. custom OpenAI-compatible endpoints) leave
	// cfg.Provider.Model unset, matching prior behavior.
	if cfg.Provider.Model == "" {
		model, err := provider.Default.ResolveDefaultModel(context.Background(), cfg)
		switch {
		case err == nil:
			cfg.Provider.Model = model
			if cfg.Provider.Default == "ollama" {
				fmt.Fprintf(os.Stderr, "Using Ollama model: %s\n", model)
			}
		case errors.Is(err, provider.ErrNoDefaultModel):
			// No default-model resolution for this provider — leave Model
			// empty, same as before this provider had a Registry entry.
		default:
			return nil, fmt.Errorf("resolving default model: %w", err)
		}
	}

	return cfg, nil
}

// applyAPIBaseFlag injects or updates an OpenAI-compatible provider entry so
// that --api-base (and optionally --api-key) work without requiring TOML
// config. If the current default provider already has a matching entry, its
// BaseURL is overwritten; otherwise a new entry is appended.
func applyAPIBaseFlag(cfg *config.Config) {
	name := cfg.Provider.Default
	// Z.ai has its own config struct — apply directly.
	if name == "zai" {
		cfg.Provider.Zai.BaseURL = apiBaseFlag
		if apiKeyFlag != "" {
			cfg.Provider.Zai.APIKeySource = "config"
			cfg.Provider.Zai.APIKey = apiKeyFlag
		}
		return
	}
	// For built-in providers that don't use the openai_compatible list,
	// synthesise a provider name so the entry can be looked up later.
	if name == "anthropic" || name == "ollama" || name == "" {
		name = "custom"
		cfg.Provider.Default = name
	}

	apiKey := apiKeyFlag
	for i, oc := range cfg.Provider.OpenAI {
		if oc.Name == name {
			cfg.Provider.OpenAI[i].BaseURL = apiBaseFlag
			if apiKey != "" {
				cfg.Provider.OpenAI[i].APIKeySource = "config"
				cfg.Provider.OpenAI[i].APIKey = apiKey
			}
			return
		}
	}

	// No existing entry — create one. Default API key to "none" so local
	// servers that need no auth work without an env-var lookup failure.
	key := apiKey
	if key == "" {
		key = "none"
	}
	cfg.Provider.OpenAI = append(cfg.Provider.OpenAI, config.OpenAICompatibleConfig{
		Name:         name,
		BaseURL:      apiBaseFlag,
		APIKeySource: "config",
		APIKey:       key,
	})
}

// applyAPIKeyFlag overrides the API key for the current default provider
// when --api-key is given without --api-base.
func applyAPIKeyFlag(cfg *config.Config) {
	name := cfg.Provider.Default
	switch name {
	case "anthropic":
		cfg.Provider.Anthropic.APIKeySource = "config"
		cfg.Provider.Anthropic.APIKey = apiKeyFlag
		return
	case "ollama":
		// Ollama doesn't use API keys; ignore silently.
		return
	case "zai":
		cfg.Provider.Zai.APIKeySource = "config"
		cfg.Provider.Zai.APIKey = apiKeyFlag
		return
	}
	for i, oc := range cfg.Provider.OpenAI {
		if oc.Name == name {
			cfg.Provider.OpenAI[i].APIKeySource = "config"
			cfg.Provider.OpenAI[i].APIKey = apiKeyFlag
			return
		}
	}
}

// dialCmux connects to the cmux daemon if running inside cmux.
// Returns a Caller (nil if not in cmux) and a cleanup function.
func dialCmux(caps *terminal.Caps) (cmux.Caller, func()) {
	if !caps.CmuxSocket {
		return nil, func() {}
	}
	cc, err := cmux.Dial(cmux.SocketPath())
	if err != nil {
		log.Printf("warning: cmux socket detected but dial failed: %v — cmux features disabled", err)
		return nil, func() {}
	}
	// Set sidebar accent color to rubichan's pink theme.
	cmux.CallerSetSidebarColor(cc, "#FF6B9D")

	return cc, func() { cc.Close() }
}

func runInteractive() error {
	caps := terminal.Detect()
	cmuxClient, closeCmux := dialCmux(caps)
	defer closeCmux()

	// Resolve config path early for bootstrap check.
	cfgDir, err := configDir()
	if err != nil {
		return err
	}
	runCtx, cancelRun := context.WithCancelCause(context.Background())
	defer cancelRun(nil)
	sessionLog, err := diag.StartSessionLogger(cfgDir, debugMode)
	if err != nil {
		return err
	}
	structuredEventLog, err := diag.StartEventLogger(eventLogPath)
	if err != nil {
		if closeErr := sessionLog.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to close session log: %v\n", closeErr)
		}
		return err
	}
	stopSignals := startInteractiveSignalHandler(cfgDir, sessionLog.Path(), cancelRun)
	defer stopSignals()
	defer func() {
		if closeErr := sessionLog.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to close session log: %v\n", closeErr)
		}
		if closeErr := structuredEventLog.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to close event log: %v\n", closeErr)
		}
	}()

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
		prog := tea.NewProgram(form, tea.WithContext(runCtx))
		finalModel, err := prog.Run()
		if err := handleInteractiveProgramError(err, runCtx, "bootstrap wizard"); err != nil {
			return err
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
	if err := interactiveExitError(runCtx); err != nil {
		return err
	}

	// Create provider
	p, err := provider.NewProviderWithDebug(cfg, debugMode)
	if err != nil {
		return fmt.Errorf("creating provider: %w", err)
	}
	if err := interactiveExitError(runCtx); err != nil {
		return err
	}

	// Set up effective working directory (creates worktree if --worktree is set).
	cwd, wtMgr, wtCleanup, err := setupWorkingDir(cfg)
	if err != nil {
		return fmt.Errorf("worktree setup: %w", err)
	}
	defer wtCleanup()
	if err := interactiveExitError(runCtx); err != nil {
		return err
	}

	// Detect and load bootstrap context if available
	bootstrapPath := filepath.Join(cwd, ".knowledge", ".bootstrap.json")
	var bootstrapContext *knowledgegraph.BootstrapMetadata
	if _, err := os.Stat(bootstrapPath); err == nil {
		ctx, err := agent.LoadBootstrapContext(bootstrapPath)
		if err == nil {
			bootstrapContext = ctx
			// Remove marker so we don't re-inject on next run
			markerPath := filepath.Join(cwd, ".knowledge", ".bootstrap-agent-start")
			os.Remove(markerPath)
		}
	}

	// Create tool registry
	registry := tools.NewRegistry()
	diffTracker := tools.NewDiffTracker()
	allowed := parseToolsFlag(toolsFlag)
	modelCaps := provider.DetectCapabilities(cfg.Provider.Default, cfg.Provider.Model)
	toolsCfg := ToolsConfig{
		ModelCapabilities: modelCaps,
		ProjectContext: ProjectContext{
			AppleProjectDetected: xcode.DiscoverProject(cwd).Type != "none",
			AppleSkillRequested:  containsSkill("apple-dev", skillsFlag),
		},
		CLIOverrides: allowed,
	}

	coreResult, err := registerCoreTools(cwd, registry, cfg, toolsCfg, diffTracker, 120*time.Second)
	if err != nil {
		return err
	}
	for _, cleanup := range coreResult.cleanups {
		defer cleanup()
	}

	// Auto-activate apple-dev Xcode tools if Apple project detected.
	var opts []agent.AgentOption
	opts = append(opts, agent.WithDiffTracker(diffTracker))
	opts = appendWorkingDirOption(opts, cwd)
	opts = append(opts, agent.WithCapabilities(modelCaps))

	// Inject bootstrap context into system prompt if available
	if bootstrapContext != nil {
		opts = append(opts, agent.WithBootstrapContext(bootstrapContext))
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

	subagentWiring, err := subagents.Wire(subagents.Options{
		Config:          cfg,
		Registry:        registry,
		EnableTask:      toolsCfg.ShouldEnable("task"),
		EnableListTasks: toolsCfg.ShouldEnable("list_tasks"),
		WorktreeManager: wtMgr,
		GitRoot:         gitRepoRoot,
		Logf:            log.Printf,
	})
	if err != nil {
		return fmt.Errorf("wiring subagents: %w", err)
	}
	agentDefReg := subagentWiring.AgentDefs
	wakeManager := subagentWiring.WakeManager
	spawner := subagentWiring.Spawner

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
	if err := folderaccess.EnsureApprovedInteractive(s, cwd, os.Stdin, os.Stderr, autoApprove, approveCwd); err != nil {
		return err
	}
	opts = append(opts, agent.WithStore(s))
	if forkFlag {
		if resumeFlag == "" {
			return fmt.Errorf("--fork requires --resume <session-id>")
		}
		newID := uuid.New().String()
		if err := s.ForkSession(resumeFlag, newID); err != nil {
			return fmt.Errorf("fork session: %w", err)
		}
		log.Printf("Forked session %s → %s", resumeFlag, newID)
		resumeFlag = newID
	}
	if resumeFlag != "" {
		opts = append(opts, agent.WithResumeSession(resumeFlag))
	}

	opts = appendPersonaOptions(opts, cwd)
	opts = appendKnowledgeGraphOption(runCtx, opts, cwd)
	opts = append(opts, agent.WithMode("interactive"))

	// Create skill runtime with built-in prompt skills and any explicit --skills.
	rt, storeCloser, err := createSkillRuntime(runCtx, registry, p, cfg, "interactive", cwd, cfgDir)
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

	// Build user hooks from config, .agent/hooks.toml, and AGENT.md.
	userHookConfigs := loadProjectHooks(cfg, s, cwd)
	if len(userHookConfigs) > 0 {
		opts = append(opts, agent.WithUserHooks(hooks.NewUserHookRunner(userHookConfigs, cwd)))
	}

	// Wire cross-session memory and summarizer.
	summaryModel := cfg.Provider.SummaryModel
	if summaryModel == "" {
		summaryModel = cfg.Provider.Model
	}
	summarizer := agent.NewLLMSummarizer(p, summaryModel)
	opts = append(opts, agent.WithSummarizer(summarizer))
	opts = append(opts, agent.WithMemoryStore(&storeMemoryAdapter{store: s}))

	// Create command registry and register built-in slash commands.
	// Built-in registration failures indicate a programming bug (duplicate names).
	cmdRegistry := commands.NewRegistry()
	for _, cmd := range []commands.SlashCommand{
		commands.NewQuitCommand(),
		commands.NewExitCommand(),
		commands.NewConfigCommand(),
		commands.NewAboutCommand(),
		commands.NewInitKnowledgeGraphCommand(),
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

	var plainHost *plainInteractiveHost
	if plainInteractive {
		plainHost = newPlainInteractiveHost(os.Stdin, os.Stdout, cfg.Provider.Model, cfg.Agent.MaxTurns, cmdRegistry)
		plainHost.SetDebug(debugMode)
		if rt != nil {
			plainHost.SetSkillRuntime(rt)
		}
	}

	// Create checkpoint manager for undo/rewind support.
	cpSessionID := uuid.New().String()
	cpMgr, err := checkpoint.New(cwd, cpSessionID, 0)
	if err != nil {
		return fmt.Errorf("create checkpoint manager: %w", err)
	}
	defer func() {
		if err := cpMgr.Cleanup(); err != nil {
			log.Printf("checkpoint cleanup: %v", err)
		}
	}()

	// Create TUI model first (with nil agent) so we can extract the
	// interactive approval function before constructing the agent.
	model := tui.NewModel(nil, "rubichan", cfg.Provider.Model, cfg.Agent.MaxTurns, cfgPath, cfg, cmdRegistry)
	model.SetTermCaps(caps)
	model.SetCmuxClient(cmuxClient)
	model.SetCheckpointManager(cpMgr) // TUI-only: enables /undo overlay; plainHost uses /rewind directly
	model.SetSessionStore(s)
	model.SetDebug(debugMode)
	if rt != nil {
		model.SetSkillSummaryProvider(rt)
	}
	sink := diag.BuildEventSink(structuredEventLog, debugMode)
	model.SetEventSink(sink)
	if plainHost != nil {
		plainHost.SetEventSink(sink)
	}
	if plainTUI {
		noAltScreen = true
		noMouse = true
		model.SetPlainMode(true)
	}

	// Set git branch in status bar if available.
	if branch, err := detectGitBranch(cwd); err == nil && branch != "" {
		model.SetGitBranch(branch)
		if plainHost != nil {
			plainHost.SetGitBranch(branch)
		}
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
		if plainHost != nil {
			plainHost.SetModel(name)
		}
	})); err != nil {
		return fmt.Errorf("register built-in command %q: %w", "model", err)
	}
	if err := cmdRegistry.Register(commands.NewDebugVerificationSnapshotCommand(func() string {
		if plainHost != nil {
			return plainHost.DebugVerificationSnapshot()
		}
		return model.DebugVerificationSnapshot()
	})); err != nil {
		return fmt.Errorf("register built-in command %q: %w", "debug-verification-snapshot", err)
	}
	if rt != nil {
		if err := cmdRegistry.Register(commands.NewSkillCommand(&skillListerAdapter{rt: rt})); err != nil {
			return fmt.Errorf("register built-in command %q: %w", "skill", err)
		}
		if err := cmdRegistry.Register(commands.NewSkillLogCommand(&skillListerAdapter{rt: rt})); err != nil {
			return fmt.Errorf("register built-in command %q: %w", "skill-log", err)
		}
	}

	// Register remaining commands that need post-model dependencies.
	// NewUndoOverlayCommand handles /undo via the TUI overlay (shows checkpoint
	// list). NewUndoCommand is not registered here to avoid a name collision —
	// the overlay supersedes the direct command in interactive mode.
	for _, cmd := range []commands.SlashCommand{
		commands.NewResumeCommand(),
		commands.NewUndoOverlayCommand(),
		commands.NewRewindCommand(cpMgr),
		commands.NewContextCommand(func() agentsdk.ContextBudget {
			if model.GetAgent() != nil {
				return model.GetAgent().ContextBudget()
			}
			return agentsdk.ContextBudget{}
		}),
		commands.NewCompactCommand(func(ctx context.Context) (agentsdk.CompactResult, error) {
			if model.GetAgent() != nil {
				return model.GetAgent().ForceCompact(ctx)
			}
			return agentsdk.CompactResult{}, nil
		}),
		commands.NewSessionsCommand(func() ([]store.Session, error) {
			if model.GetAgent() != nil {
				return model.GetAgent().ListSessions(20)
			}
			return nil, nil
		}),
		commands.NewForkCommand(func(ctx context.Context) (string, error) {
			if model.GetAgent() != nil {
				return model.GetAgent().ForkSession(ctx)
			}
			return "", fmt.Errorf("no agent")
		}),
	} {
		if err := cmdRegistry.Register(cmd); err != nil {
			return fmt.Errorf("register built-in command %q: %w", cmd.Name(), err)
		}
	}

	// Build tool execution slot middlewares first so the rule engine can
	// feed the approval system. The agent composes the full pipeline.
	pc := buildPipeline(registry, cfg, cwd, rt)
	opts = append(opts, agent.WithToolMiddlewares(pc.Middlewares))

	if !autoApprove {
		if plainHost != nil {
			approvalFunc = plainHost.MakeApprovalFunc()
		} else {
			// UIRequestHandler takes priority in the shared agentsdk.ApprovalFlow.
			// Keep approvalFunc wired as a fallback for non-UI handler paths.
			approvalFunc = model.MakeApprovalFunc()
			opts = append(opts, agent.WithUIRequestHandler(model.MakeUIRequestHandler()))
		}

		// Build the approval checker: compose session cache, pipeline rule
		// engine, and config-based trust rules. Session cache (TUI "always"
		// decisions) is checked first, then the pipeline's rule engine
		// (category-based allow rules), then config trust rules.
		var checkers []agent.ApprovalChecker

		// Hierarchical permission policies (org → project → user).
		if hc := buildHierarchicalChecker(cfg, cfgPath, cwd); hc != nil {
			checkers = append(checkers, hc)
		}

		if plainHost != nil {
			checkers = append(checkers, plainHost)
		} else {
			checkers = append(checkers, model) // session cache
		}
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
		// Auto-approve mode: still respect hierarchical deny policies.
		var autoCheckers []agent.ApprovalChecker
		if hc := buildHierarchicalChecker(cfg, cfgPath, cwd); hc != nil {
			autoCheckers = append(autoCheckers, hc)
		}
		autoCheckers = append(autoCheckers, agent.AlwaysAutoApprove{})
		composite := agent.NewCompositeApprovalChecker(autoCheckers...)
		opts = append(opts, agent.WithApprovalChecker(composite))
		spawner.ApprovalChecker = composite
	}

	// Attach the wake manager for background subagent notifications.
	opts = append(opts, agent.WithWakeManager(wakeManager))

	// Create shared rate limiter for throttling LLM API requests.
	var rateLimiter *agent.SharedRateLimiter
	if cfg.Agent.MaxRequestsPerMinute > 0 {
		rateLimiter = agent.NewSharedRateLimiter(cfg.Agent.MaxRequestsPerMinute)
	}
	if rateLimiter != nil {
		opts = append(opts, agent.WithRateLimiter(rateLimiter))
	}

	opts = append(opts, agent.WithCheckpointManager(cpMgr))

	// Create agent with the approval function.
	a := agent.New(p, registry, approvalFunc, cfg, opts...)

	// Wire spawner dependencies that need the agent and provider.
	spawner.Provider = p
	spawner.ParentTools = registry
	spawner.ParentSkillRuntime = rt
	spawner.RateLimiter = rateLimiter

	// Register notes tool backed by agent's scratchpad.
	if toolsCfg.ShouldEnable("notes") {
		if err := registry.Register(tools.NewNotesTool(a.ScratchpadAccess())); err != nil {
			return fmt.Errorf("registering notes tool: %w", err)
		}
	}

	// Register task_complete tool for explicit loop termination.
	if toolsCfg.ShouldEnable("task_complete") {
		if err := registry.Register(tools.NewCompletionSignalTool()); err != nil {
			return fmt.Errorf("registering task_complete tool: %w", err)
		}
	}

	// Register cmux tools when running inside cmux terminal.
	if cmuxClient != nil {
		cmuxTools := []tools.Tool{
			tools.NewCmuxBrowserNavigate(cmuxClient),
			tools.NewCmuxBrowserSnapshot(cmuxClient),
			tools.NewCmuxBrowserClick(cmuxClient),
			tools.NewCmuxBrowserType(cmuxClient),
			tools.NewCmuxBrowserWait(cmuxClient),
			tools.NewCmuxSplit(cmuxClient),
			tools.NewCmuxSend(cmuxClient),
			tools.NewCmuxOrchestrate(cmuxClient),
		}
		for _, t := range cmuxTools {
			if toolsCfg.ShouldEnable(t.Name()) {
				if err := registry.Register(t); err != nil {
					log.Printf("warning: registering cmux tool %q: %v", t.Name(), err)
				}
			}
		}
	}

	// Wire the agent into the TUI model now that both exist.
	model.SetAgent(a)
	model.SetWikiConfig(tui.WikiCommandConfig{
		WorkDir: cwd,
		LLM:     llmCompleter,
	})
	if plainHost != nil {
		plainHost.SetAgent(a)
		err := plainHost.Run(runCtx)
		saveMemoriesBestEffort(runCtx, a, os.Stderr)
		if err != nil {
			return err
		}
		return interactiveExitError(runCtx)
	}

	// Index project files for @ mention autocomplete (background).
	fileSrc := tui.NewFileCompletionSource(cwd)
	model.SetFileCompletionSource(fileSrc)
	go indexProjectFiles(cwd, fileSrc)

	programOpts := []tea.ProgramOption{
		tea.WithContext(runCtx),
	}
	if !noMouse {
		programOpts = append(programOpts, tea.WithMouseCellMotion())
	}
	if !noAltScreen {
		programOpts = append(programOpts, tea.WithAltScreen())
	}
	// TODO: enable Kitty keyboard (caps.KittyKeyboard) when bubbletea adds tea.WithKittyKeyboard().
	prog := tea.NewProgram(model, programOpts...)
	if _, err := prog.Run(); err != nil {
		if err := handleInteractiveProgramError(err, runCtx, "running TUI"); err != nil {
			return err
		}
	}
	if err := interactiveExitError(runCtx); err != nil {
		return err
	}

	// Save memories on graceful shutdown.
	saveMemoriesBestEffort(runCtx, a, os.Stderr)
	if err := interactiveExitError(runCtx); err != nil {
		return err
	}

	return nil
}

func runHeadless() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	caps := terminal.Detect()
	cmuxClient, closeCmux := dialCmux(caps)
	defer closeCmux()

	if maxTurnsFlag > 0 {
		cfg.Agent.MaxTurns = maxTurnsFlag
	}

	// Single top-level timeout governs the entire headless execution.
	ctx, cancel := context.WithTimeout(context.Background(), timeoutFlag)
	defer cancel()

	// Set up effective working directory (creates worktree if --worktree is set).
	cwd, wtMgr, wtCleanup, err := setupWorkingDir(cfg)
	if err != nil {
		return fmt.Errorf("worktree setup: %w", err)
	}
	defer wtCleanup()

	// Emit OSC 7 unconditionally — terminals that don't support it silently ignore
	// unknown sequences, and there's no visual side effect. This sets the tab
	// title / breadcrumb path in terminals that support it (Ghostty, iTerm2, etc.).
	terminal.SetWorkingDirectory(os.Stderr, cwd)

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
	p, err := provider.NewProviderWithDebug(cfg, debugMode)
	if err != nil {
		return fmt.Errorf("creating provider: %w", err)
	}

	registry := tools.NewRegistry()
	allowed := parseToolsFlag(toolsFlag)
	headlessDiffTracker := tools.NewDiffTracker()
	headlessCaps := provider.DetectCapabilities(cfg.Provider.Default, cfg.Provider.Model)
	headlessToolsCfg := ToolsConfig{
		ModelCapabilities: headlessCaps,
		ProjectContext: ProjectContext{
			AppleProjectDetected: xcode.DiscoverProject(cwd).Type != "none",
			AppleSkillRequested:  containsSkill("apple-dev", skillsFlag),
		},
		CLIOverrides: allowed,
		HeadlessMode: true,
	}

	headlessCoreResult, err := registerCoreTools(cwd, registry, cfg, headlessToolsCfg, headlessDiffTracker, timeoutFlag)
	if err != nil {
		return err
	}
	for _, cleanup := range headlessCoreResult.cleanups {
		defer cleanup()
	}

	// Auto-activate apple-dev Xcode tools if Apple project detected.
	var opts []agent.AgentOption
	opts = append(opts, agent.WithDiffTracker(headlessDiffTracker))
	opts = appendWorkingDirOption(opts, cwd)
	opts = append(opts, agent.WithCapabilities(headlessCaps))
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
	structuredEventLog, err := diag.StartEventLogger(eventLogPath)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := structuredEventLog.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to close event log: %v\n", closeErr)
		}
	}()
	s, err := openStore(cfgDir)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}
	defer s.Close()
	if err := folderaccess.EnsureApprovedNonInteractive(s, cwd, autoApprove, approveCwd); err != nil {
		return err
	}
	opts = append(opts, agent.WithStore(s))
	if forkFlag {
		if resumeFlag == "" {
			return fmt.Errorf("--fork requires --resume <session-id>")
		}
		newID := uuid.New().String()
		if err := s.ForkSession(resumeFlag, newID); err != nil {
			return fmt.Errorf("fork session: %w", err)
		}
		log.Printf("Forked session %s → %s", resumeFlag, newID)
		resumeFlag = newID
	}
	if resumeFlag != "" {
		opts = append(opts, agent.WithResumeSession(resumeFlag))
	}

	opts = appendPersonaOptions(opts, cwd)

	// Create skill runtime with built-in skills.
	headlessMode := modeFlag
	if headlessMode == "" {
		headlessMode = "headless"
	}
	opts = append(opts, agent.WithMode(headlessMode))
	rt, storeCloser, err := createSkillRuntime(ctx, registry, p, cfg, headlessMode, cwd, cfgDir)
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

	// Build user hooks from config, .agent/hooks.toml, and AGENT.md.
	headlessHookConfigs := loadProjectHooks(cfg, s, cwd)
	if len(headlessHookConfigs) > 0 {
		opts = append(opts, agent.WithUserHooks(hooks.NewUserHookRunner(headlessHookConfigs, cwd)))
	}

	// Wire cross-session memory and summarizer.
	headlessSummaryModel := cfg.Provider.SummaryModel
	if headlessSummaryModel == "" {
		headlessSummaryModel = cfg.Provider.Model
	}
	headlessSummarizer := agent.NewLLMSummarizer(p, headlessSummaryModel)
	opts = append(opts, agent.WithSummarizer(headlessSummarizer))
	opts = append(opts, agent.WithMemoryStore(&storeMemoryAdapter{store: s}))

	headlessWiring, err := subagents.Wire(subagents.Options{
		Config:          cfg,
		Registry:        registry,
		EnableTask:      headlessToolsCfg.ShouldEnable("task"),
		EnableListTasks: headlessToolsCfg.ShouldEnable("list_tasks"),
		WorktreeManager: wtMgr,
		GitRoot:         gitRepoRoot,
		Logf:            log.Printf,
	})
	if err != nil {
		return fmt.Errorf("wiring subagents: %w", err)
	}
	headlessWakeManager := headlessWiring.WakeManager
	headlessSpawner := headlessWiring.Spawner

	// Headless auto-approves all tools (restricted via --tools allowlist),
	// but still respects hierarchical deny policies so org-level restrictions
	// apply in CI/CD.
	{
		var headlessCheckers []agent.ApprovalChecker
		if hc := buildHierarchicalChecker(cfg, configPath, cwd); hc != nil {
			headlessCheckers = append(headlessCheckers, hc)
		}
		headlessCheckers = append(headlessCheckers, agent.AlwaysAutoApprove{})
		composite := agent.NewCompositeApprovalChecker(headlessCheckers...)
		opts = append(opts, agent.WithApprovalChecker(composite))
		headlessSpawner.ApprovalChecker = composite
	}
	opts = append(opts, agent.WithParallelPolicy(agent.AllowAllParallel{}))
	opts = append(opts, agent.WithWakeManager(headlessWakeManager))

	// Build tool execution slot middlewares; the agent composes the pipeline.
	hpc := buildPipeline(registry, cfg, cwd, rt)
	opts = append(opts, agent.WithToolMiddlewares(hpc.Middlewares))

	// Create shared rate limiter for throttling LLM API requests.
	var headlessRateLimiter *agent.SharedRateLimiter
	if cfg.Agent.MaxRequestsPerMinute > 0 {
		headlessRateLimiter = agent.NewSharedRateLimiter(cfg.Agent.MaxRequestsPerMinute)
		opts = append(opts, agent.WithRateLimiter(headlessRateLimiter))
	}

	a := agent.New(p, registry, approvalFunc, cfg, opts...)

	// Wire spawner dependencies that need the agent and provider.
	headlessSpawner.Provider = p
	headlessSpawner.ParentTools = registry
	headlessSpawner.ParentSkillRuntime = rt
	headlessSpawner.RateLimiter = headlessRateLimiter

	// Register notes tool backed by agent's scratchpad.
	if headlessToolsCfg.ShouldEnable("notes") {
		if err := registry.Register(tools.NewNotesTool(a.ScratchpadAccess())); err != nil {
			return fmt.Errorf("registering notes tool: %w", err)
		}
	}

	// Register task_complete tool for explicit loop termination.
	if headlessToolsCfg.ShouldEnable("task_complete") {
		if err := registry.Register(tools.NewCompletionSignalTool()); err != nil {
			return fmt.Errorf("registering task_complete tool: %w", err)
		}
	}

	// Run headless
	mode := modeFlag
	if mode == "" {
		mode = "generic"
	}

	// Run LLM review and security scan concurrently for code-review mode.
	hr := runner.NewHeadlessRunner(a.Turn)
	hr.SetModelName(cfg.Provider.Model)
	if sink := diag.BuildEventSink(structuredEventLog, debugMode); len(sink) > 0 {
		hr.SetEventSink(sink)
	}
	promptText = applyHeadlessBootstrapProbePrompt(promptText, headlessToolsCfg.ShouldEnable("shell"))
	var result *output.RunResult
	var secReport *security.Report

	if mode == "code-review" {
		engineCfg := security.EngineConfig{
			Concurrency:     4,
			MaxLLMChunks:    cfg.Security.MaxLLMCalls,
			ExcludePatterns: cfg.Security.ExcludePatterns,
		}
		if rt != nil {
			engineCfg.OnScanComplete = securityScanCompleteHook(rt)
		}
		// Only pass the provider when LLM analysis is explicitly enabled;
		// the config defaults to false so users opt-in to the cost.
		var llmForSec provider.LLMProvider
		if cfg.Security.EnableLLMAnalysis {
			llmForSec = p
		}
		engine := newDefaultSecurityEngine(engineCfg, llmForSec)

		// Load .security.yaml for custom rules.
		projectCfg, projectCfgErr := security.LoadProjectConfig(cwd)
		if projectCfgErr != nil {
			fmt.Fprintf(os.Stderr, "warning: loading .security.yaml: %v\n", projectCfgErr)
		}
		if projectCfg != nil && len(projectCfg.Rules) > 0 {
			engine.AddScanner(scanner.NewCustomRuleScanner(projectCfg.Rules))
		}

		// Add skill-provided security scanners so security-rule skills
		// participate in the same engine used by review workflows.
		if rt != nil {
			for _, sc := range rt.GetScanners() {
				engine.AddScanner(&skillScannerAdapter{scanner: sc})
			}
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
	if err := validateHeadlessBootstrapProbe(result, headlessToolsCfg.ShouldEnable("shell")); err != nil {
		return err
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

	// Terminal notifications for headless completion.
	// Try cmux first; fall back to OSC terminal notifications on failure.
	notified := cmuxClient != nil && cmux.CallerNotify(cmuxClient, "Rubichan", "Code Review", "Code review complete")
	if cmuxClient != nil && secReport != nil {
		summary := secReport.Summary()
		highSeverityCount := summary.Critical + summary.High
		if highSeverityCount > 0 {
			cmux.CallerNotify(cmuxClient, "Rubichan", "", fmt.Sprintf("%d high-severity security findings detected", highSeverityCount))
		}
	}
	if !notified && caps.Notifications {
		terminal.Notify(os.Stderr, "Code review complete")
		if secReport != nil {
			summary := secReport.Summary()
			highSeverityCount := summary.Critical + summary.High
			if highSeverityCount > 0 {
				terminal.Notify(os.Stderr, fmt.Sprintf("%d high-severity security findings detected", highSeverityCount))
			}
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

	// Post results to PR if requested.
	if postToPRFlag {
		if err := postResultsToPR(ctx, result, secReport); err != nil {
			return fmt.Errorf("failed to post to PR: %w", err)
		}
	}

	// Upload SARIF independently of --post-to-pr.
	if uploadSARIFFlag && !postToPRFlag && secReport != nil {
		if err := uploadSARIFStandalone(ctx, secReport); err != nil {
			return fmt.Errorf("failed to upload SARIF: %w", err)
		}
	}

	// Emit GitHub Actions annotations if requested or auto-detected.
	// Annotations go to stderr to avoid polluting structured stdout output (e.g., --output json).
	if annotationsFlag || (os.Getenv("GITHUB_ACTIONS") == "true" && len(result.SecurityFindings) > 0) {
		annotFmt := output.NewGitHubAnnotationsFormatter()
		annotOut, err := annotFmt.Format(result)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to format annotations: %v\n", err)
		} else if len(annotOut) > 0 {
			fmt.Fprint(os.Stderr, string(annotOut))
		}
	}

	// Save memories on completion.
	saveMemoriesBestEffort(ctx, a, os.Stderr)

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

func applyHeadlessBootstrapProbePrompt(prompt string, shellEnabled bool) string {
	if !shellEnabled {
		return prompt
	}
	prefix := strings.TrimSpace(`
Headless bootstrap requirement:
- Before any analysis or final response, execute exactly one read-only shell tool call: {"command":"pwd"}.
- Do not ask for permission and do not skip this step.
- After this bootstrap probe succeeds, continue with the requested task.
`)
	return prefix + "\n\nUser task:\n" + strings.TrimSpace(prompt)
}

func validateHeadlessBootstrapProbe(result *output.RunResult, shellEnabled bool) error {
	if !shellEnabled {
		return nil
	}
	if result == nil || len(result.ToolCalls) == 0 {
		return fmt.Errorf("headless bootstrap probe missing: expected first tool call shell {\"command\":\"pwd\"}")
	}
	first := result.ToolCalls[0]
	if first.Name != "shell" {
		return fmt.Errorf("headless bootstrap probe invalid: first tool call must be shell {\"command\":\"pwd\"}, got %q", first.Name)
	}
	if !isShellPwdProbe(first.Input) {
		return fmt.Errorf("headless bootstrap probe invalid: expected shell {\"command\":\"pwd\"} as first tool call")
	}
	return nil
}

func isShellPwdProbe(input json.RawMessage) bool {
	var payload struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return false
	}
	return strings.TrimSpace(payload.Command) == "pwd"
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
	ModelCapabilities provider.ModelCapabilities
	UserPreferences   UserToolPrefs
	ProjectContext    ProjectContext
	FeatureFlags      map[string]bool
	CLIOverrides      map[string]bool
	// HeadlessMode enables default-deny behavior: when true and CLIOverrides
	// is nil, ShouldEnable returns false for all tools. This requires headless
	// callers to provide an explicit --tools allowlist or named profile.
	HeadlessMode bool
}

// ShouldEnable returns true if a tool should be registered. Tools are always
// registered regardless of SupportsNativeToolUse — the agent loop handles
// the distinction between native tool_use and text-based XML fallback.
func (tc ToolsConfig) ShouldEnable(name string) bool {
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

// coreToolsResult holds the artifacts produced by registerCoreTools.
type coreToolsResult struct {
	cleanups []func() // cleanup functions that must be deferred by the caller
}

// registerCoreTools registers the standard tool set (file, shell, search,
// process, extended, LSP) into the given registry. It is shared between
// interactive and headless modes to eliminate duplication.
func registerCoreTools(cwd string, registry *tools.Registry, cfg *config.Config, toolsCfg ToolsConfig, diffTracker *tools.DiffTracker, shellTimeout time.Duration) (*coreToolsResult, error) {
	result := &coreToolsResult{}

	var fileTool *tools.FileTool
	if toolsCfg.ShouldEnable("file") {
		fileTool = tools.NewFileTool(cwd)
		fileTool.SetDiffTracker(diffTracker)
		if err := registry.Register(fileTool); err != nil {
			return nil, fmt.Errorf("registering file tool: %w", err)
		}
	}

	if toolsCfg.ShouldEnable("shell") {
		shellTool := tools.NewShellTool(cwd, shellTimeout)
		shellTool.SetDiffTracker(diffTracker)
		shellTool.SetSandbox(tools.NewDefaultShellSandbox(cwd))

		if cleanup, err := wireSandboxProxy(cfg, shellTool); err != nil {
			return nil, err
		} else if cleanup != nil {
			result.cleanups = append(result.cleanups, cleanup)
		}

		if err := registry.Register(shellTool); err != nil {
			return nil, fmt.Errorf("registering shell tool: %w", err)
		}
	}
	if toolsCfg.ShouldEnable("search") {
		if err := registry.Register(tools.NewSearchTool(cwd)); err != nil {
			return nil, fmt.Errorf("registering search tool: %w", err)
		}
	}
	if toolsCfg.ShouldEnable("process") {
		procMgr := tools.NewProcessManager(cwd, tools.ProcessManagerConfig{})
		result.cleanups = append(result.cleanups, func() { _ = procMgr.Shutdown(context.Background()) })
		if err := registry.Register(tools.NewProcessTool(procMgr)); err != nil {
			return nil, fmt.Errorf("registering process tool: %w", err)
		}
	}
	// Register common aliases so models that hallucinate tool names like
	// "run_command" or "write_file" still resolve to the correct tool.
	tools.RegisterDefaultAliases(registry)

	if err := wireExtendedTools(cwd, registry, cfg, toolsCfg); err != nil {
		return nil, err
	}

	if lspManager, cleanup, err := wireLSPTools(cfg, registry, toolsCfg, cwd); err != nil {
		return nil, err
	} else {
		if cleanup != nil {
			result.cleanups = append(result.cleanups, cleanup)
		}
		if lspManager != nil && fileTool != nil {
			fileTool.SetLSPNotifier(&lsp.ManagerNotifier{Manager: lspManager})
		}
	}

	return result, nil
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

func wireExtendedTools(cwd string, registry *tools.Registry, cfg *config.Config, toolsCfg ToolsConfig) error {
	for _, tool := range []tools.Tool{
		httptool.NewGetTool(),
		httptool.NewPostTool(),
		httptool.NewPutTool(),
		httptool.NewPatchTool(),
		httptool.NewDeleteTool(),
		httptool.NewWebFetchTool(),
		gittools.NewStatusTool(cwd),
		gittools.NewDiffTool(cwd),
		gittools.NewLogTool(cwd),
		gittools.NewShowTool(cwd),
		gittools.NewBlameTool(cwd),
		dbtools.NewQueryTool(cwd),
	} {
		if toolsCfg.ShouldEnable(tool.Name()) {
			if err := registry.Register(tool); err != nil {
				return fmt.Errorf("registering tool %s: %w", tool.Name(), err)
			}
		}
	}

	var enabledBrowserTools []tools.Tool
	for _, tool := range browser.NewTools(nil) {
		if toolsCfg.ShouldEnable(tool.Name()) {
			enabledBrowserTools = append(enabledBrowserTools, tool)
		}
	}
	if len(enabledBrowserTools) == 0 {
		return nil
	}

	browserService, err := browser.NewService(cwd, cfg.Browser, cfg.MCP.Servers)
	if err != nil {
		return fmt.Errorf("create browser service: %w", err)
	}
	for _, tool := range browser.NewTools(browserService) {
		if toolsCfg.ShouldEnable(tool.Name()) {
			if err := registry.Register(tool); err != nil {
				return fmt.Errorf("registering browser tool %s: %w", tool.Name(), err)
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

// buildHierarchicalChecker loads permission policies from org, project, and user
// config and returns a HierarchicalChecker, or nil if no policies are configured.
func buildHierarchicalChecker(cfg *config.Config, cfgPathOverride, cwd string) agent.ApprovalChecker {
	// Determine config path for user policy source attribution.
	cfgPath := cfgPathOverride
	if cfgPath == "" {
		if home, err := os.UserHomeDir(); err == nil {
			cfgPath = filepath.Join(home, ".config", "rubichan", "config.toml")
		}
	}
	configDir, _ := os.UserConfigDir()
	orgPath := filepath.Join(configDir, "aiagent", "org-policy.toml")
	projectPath := filepath.Join(cwd, ".agent", "permissions.toml")

	var userPolicy *permissions.Policy
	cp := cfg.Permissions
	if len(cp.Tools.Allow) > 0 || len(cp.Tools.Deny) > 0 || len(cp.Tools.Prompt) > 0 ||
		len(cp.Shell.AllowCommands) > 0 || len(cp.Shell.DenyCommands) > 0 || len(cp.Shell.PromptPatterns) > 0 ||
		len(cp.Files.AllowPatterns) > 0 || len(cp.Files.DenyPatterns) > 0 || len(cp.Files.PromptPatterns) > 0 ||
		len(cp.Skills.AutoApprove) > 0 || len(cp.Skills.Deny) > 0 {
		userPolicy = &permissions.Policy{
			Level:  "user",
			Source: cfgPath,
			Tools:  permissions.ToolPolicy{Allow: cp.Tools.Allow, Deny: cp.Tools.Deny, Prompt: cp.Tools.Prompt},
			Shell:  permissions.ShellPolicy{AllowCommands: cp.Shell.AllowCommands, DenyCommands: cp.Shell.DenyCommands, PromptPatterns: cp.Shell.PromptPatterns},
			Files:  permissions.FilePolicy{AllowPatterns: cp.Files.AllowPatterns, DenyPatterns: cp.Files.DenyPatterns, PromptPatterns: cp.Files.PromptPatterns},
			Skills: permissions.SkillPolicy{AutoApprove: cp.Skills.AutoApprove, Deny: cp.Skills.Deny},
		}
	}

	policies, err := permissions.LoadPolicies(orgPath, projectPath, userPolicy)
	if err != nil {
		log.Printf("warning: failed to load permission policies: %v", err)
		return nil
	}
	if len(policies) == 0 {
		return nil
	}
	checker := permissions.NewHierarchicalChecker(policies)
	mode := agentsdk.ParsePermissionMode(cfg.Permissions.Mode)
	return permissions.NewModeAwareChecker(mode, checker)
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

// detectPlatformClient auto-detects the CI environment and returns a platform client.
func detectPlatformClient() (platform.Platform, *platform.DetectedEnv, error) {
	env, err := platform.DetectFromEnv()
	if err != nil {
		return nil, nil, fmt.Errorf("detecting platform: %w", err)
	}
	if env == nil {
		return nil, nil, fmt.Errorf("could not detect git platform from environment; set GITHUB_ACTIONS or GITLAB_CI")
	}
	plat, err := platform.New(env)
	if err != nil {
		return nil, nil, fmt.Errorf("creating platform client: %w", err)
	}
	return plat, env, nil
}

func postResultsToPR(ctx context.Context, result *output.RunResult, secReport *security.Report) error {
	plat, env, err := detectPlatformClient()
	if err != nil {
		return err
	}

	// Override PR number if --pr flag was provided.
	if prNumberFlag > 0 {
		env.PRNumber = prNumberFlag
	}
	if env.PRNumber == 0 {
		return fmt.Errorf("could not detect PR number; use --pr=N to specify")
	}

	// Post unified PR comment (LLM review + security findings) via bridge.
	prFmt := output.NewPRCommentFormatter()
	if err := platform.PostRunResultComment(ctx, plat, prFmt, result, env.Repo, env.PRNumber); err != nil {
		return fmt.Errorf("posting PR comment: %w", err)
	}

	// Post inline security review comments if available.
	if secReport != nil && len(secReport.Findings) > 0 {
		prReviewFmt := secoutput.NewGitHubPRFormatter()
		if err := platform.PostSecurityReview(ctx, plat, prReviewFmt, secReport, env.Repo, env.PRNumber); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to post security review: %v\n", err)
		}
	}

	// Upload SARIF if requested and security report is available.
	if uploadSARIFFlag && secReport != nil {
		if env.CommitSHA == "" {
			fmt.Fprintln(os.Stderr, "warning: could not determine commit SHA for SARIF upload, skipping")
		} else {
			sarifFmt := secoutput.NewSARIFFormatter()
			if err := platform.UploadSecuritySARIF(ctx, plat, sarifFmt, secReport, env.Repo, env.CommitSHA, env.Ref); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to upload SARIF: %v\n", err)
			}
		}
	}

	return nil
}

// runWikiHeadless runs the wiki generation pipeline directly without an agent loop.
// It creates an LLM provider, a parser, and delegates to wiki.Run, emitting
// progress updates to stderr.
func runWikiHeadless(cfg *config.Config, cwd, outDir, format string, concurrency int) error {
	caps := terminal.Detect()
	cmuxClient, closeCmux := dialCmux(caps)
	defer closeCmux()

	p, err := provider.NewProviderWithDebug(cfg, debugMode)
	if err != nil {
		return fmt.Errorf("creating provider: %w", err)
	}

	llm := integrations.NewLLMCompleter(p, cfg.Provider.Model)
	par := parser.NewParser()

	wikiCfg := wiki.Config{
		Dir:         cwd,
		OutputDir:   outDir,
		Format:      format,
		Concurrency: concurrency,
		ProgressFunc: func(stage string, current, total int) {
			if total > 0 {
				fmt.Fprintf(os.Stderr, "[%s] %d/%d\n", stage, current, total)
				percent := current * 100 / total
				if cmuxClient != nil {
					cmux.CallerSetProgress(cmuxClient, float64(percent)/100.0, stage)
				} else if caps.ProgressBar {
					terminal.SetProgress(os.Stderr, terminal.ProgressNormal, percent)
				}
			} else {
				fmt.Fprintf(os.Stderr, "[%s]\n", stage)
				if cmuxClient != nil {
					cmux.CallerSetProgress(cmuxClient, 0.0, stage)
				} else if caps.ProgressBar {
					terminal.SetProgress(os.Stderr, terminal.ProgressIndeterminate, 0)
				}
			}
		},
	}

	result, err := wiki.Run(context.Background(), wikiCfg, llm, par)

	if cmuxClient != nil {
		cmux.CallerClearProgress(cmuxClient)
	} else if caps.ProgressBar {
		terminal.ClearProgress(os.Stderr)
	}

	if err != nil {
		if cmuxClient != nil {
			cmux.CallerNotify(cmuxClient, "Rubichan", "", "Wiki generation failed")
		} else if caps.Notifications {
			terminal.Notify(os.Stderr, "Wiki generation failed")
		}
		return err
	}

	if cmuxClient != nil {
		cmux.CallerNotify(cmuxClient, "Rubichan", "Wiki Generation", fmt.Sprintf("Wiki complete — %d documents rendered", result.Documents))
	} else if caps.Notifications {
		terminal.Notify(os.Stderr, fmt.Sprintf("Wiki complete — %d documents rendered", result.Documents))
	}

	switch outputFlag {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if encErr := enc.Encode(result); encErr != nil {
			return fmt.Errorf("encoding wiki result: %w", encErr)
		}
	default:
		fmt.Fprintf(os.Stdout, "Wiki generated: %d documents, %d diagrams — output: %s (format: %s, %.1fs)\n",
			result.Documents, result.Diagrams, result.OutputDir, result.Format,
			float64(result.DurationMs)/1000)
	}
	return nil
}

// uploadSARIFStandalone uploads SARIF without requiring --post-to-pr.
func uploadSARIFStandalone(ctx context.Context, secReport *security.Report) error {
	plat, env, err := detectPlatformClient()
	if err != nil {
		return err
	}
	if env.CommitSHA == "" {
		return fmt.Errorf("could not determine commit SHA; set GITHUB_SHA or CI_COMMIT_SHA")
	}
	sarifFmt := secoutput.NewSARIFFormatter()
	return platform.UploadSecuritySARIF(ctx, plat, sarifFmt, secReport, env.Repo, env.CommitSHA, env.Ref)
}
