package skillruntime

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/integrations"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/skills/builtin/appledev"
	"github.com/julianshen/rubichan/internal/skills/builtin/codereview"
	"github.com/julianshen/rubichan/internal/skills/builtin/frontenddesign"
	"github.com/julianshen/rubichan/internal/skills/builtin/uiuxpromax"
	"github.com/julianshen/rubichan/internal/skills/goplugin"
	"github.com/julianshen/rubichan/internal/skills/mcpbackend"
	"github.com/julianshen/rubichan/internal/skills/process"
	"github.com/julianshen/rubichan/internal/skills/sandbox"
	starengine "github.com/julianshen/rubichan/internal/skills/starlark"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/tools"
)

// Options carries what New needs from its caller. Everything here is a
// composition decision the CLI (or an embedder) has already made: which
// skills were asked for, whether they are pre-approved, and where config
// lives.
type Options struct {
	// Registry receives tools contributed by activated skills.
	Registry *tools.Registry
	// Provider backs the LLM integration offered to skills.
	Provider provider.LLMProvider
	// Config supplies skill directories, MCP servers and the activation
	// threshold. Required.
	Config *config.Config
	// Mode is the activation trigger context — "interactive", "headless",
	// "code-review", "shell", "wiki".
	Mode string
	// WorkDir is the project root: the source of project-level skills and of
	// the file list trigger evaluation sees.
	WorkDir string
	// ConfigDir is the resolved rubichan config directory. The skill store
	// and the default user skill directory live under it.
	ConfigDir string
	// SkillNames restricts discovery to these skills; empty means all.
	SkillNames []string
	// AutoApprove pre-approves SkillNames rather than prompting.
	AutoApprove bool
}

// New creates and configures a skill runtime with built-in prompt skills and
// any explicitly requested skills. Built-in skills are always registered and
// auto-activate based on mode triggers.
//
// The returned io.Closer must be closed by the caller to release the SQLite
// store.
func New(ctx context.Context, opts Options) (*skills.Runtime, io.Closer, error) {

	if opts.Config == nil {
		return nil, nil, fmt.Errorf("config is required for skill runtime")
	}

	cfg, configDir, workDir := opts.Config, opts.ConfigDir, opts.WorkDir
	skillNames := opts.SkillNames

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
	if err := RegisterBuiltinPrompts(loader, configDir); err != nil {
		return nil, nil, fmt.Errorf("register builtin skills: %w", err)
	}

	// Create integration objects shared across all skill backends.
	llmCompleter := integrations.NewLLMCompleter(opts.Provider, cfg.Provider.Model)
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

	// The caller decides whether the requested skills are pre-approved; the
	// CLI does that from --approve-skills.
	var autoApproveSkills []string
	if opts.AutoApprove {
		autoApproveSkills = skillNames
	}

	rt := skills.NewRuntime(loader, s, opts.Registry, autoApproveSkills, backendFactory, sandboxFactory)
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
		Mode:         opts.Mode,
		ProjectFiles: projectFiles,
	}
	if err := rt.EvaluateAndActivate(triggerCtx); err != nil {
		s.Close()
		return nil, nil, fmt.Errorf("activating skills: %w", err)
	}

	return rt, s, nil
}

// RegisterBuiltinPrompts registers the prompt skills that ship with rubichan.
// Exported because the skill CLI subcommands build loaders of their own and
// must see the same built-ins the runtime does.
func RegisterBuiltinPrompts(loader *skills.Loader, configDir string) error {
	frontenddesign.Register(loader)
	codereview.Register(loader)
	appledev.RegisterPrompt(loader)
	return uiuxpromax.Register(loader, configDir)
}
