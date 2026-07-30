// Package subagents wires the subagent system: the registry of agent
// definitions a task may spawn, the wake manager that reports background
// completions, the spawner that runs them, and the task tools that expose all
// of it to the model.
//
// It lives outside cmd/ because the same wiring is needed by every mode that
// can spawn subagents, and because it carries policy rather than plumbing —
// which definitions ship built in, how config-declared ones are translated,
// and whether a subagent gets its own worktree.
package subagents

import (
	"fmt"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/internal/worktree"
)

// Options carries what Wire needs from its caller.
type Options struct {
	// Config supplies the agent definitions to register and the worktree
	// limits applied to subagent isolation. Required.
	Config *config.Config
	// Registry receives the task tools. Required.
	Registry *tools.Registry
	// EnableTask and EnableListTasks mirror the caller's tool allowlist.
	// When both are false nothing is registered and no worktree provider is
	// resolved, so a caller that has disabled them pays nothing — notably no
	// git invocation.
	EnableTask      bool
	EnableListTasks bool
	// WorktreeManager is the session's own manager, when it has one. Sharing
	// it matters: two managers over the same repository each enforce
	// MaxWorktrees independently and neither sees the other's worktrees.
	// When nil, Wire discovers the repository root through GitRoot and builds
	// one, so subagents can still use isolation: "worktree" even though the
	// session itself is not running in one.
	WorktreeManager *worktree.Manager
	// GitRoot resolves the repository root for that fallback. A nil GitRoot,
	// or one that returns an error, leaves subagents without worktree
	// isolation rather than failing construction.
	GitRoot func() (string, error)
	// Logf reports agent definitions that could not be registered — a
	// duplicate name, say. Nil discards them.
	Logf func(format string, args ...any)
}

// Wiring is what Wire produced, for callers that must finish connecting it
// once the agent and provider exist.
type Wiring struct {
	AgentDefs   *agent.AgentDefRegistry
	WakeManager *agent.WakeManager
	Spawner     *agent.DefaultSubagentSpawner
}

// Wire builds the subagent system and registers the task tools the caller
// enabled.
func Wire(opts Options) (*Wiring, error) {
	if opts.Config == nil {
		return nil, fmt.Errorf("config is required for subagent wiring")
	}
	if opts.Registry == nil {
		return nil, fmt.Errorf("tool registry is required for subagent wiring")
	}

	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}

	agentDefs := agent.NewAgentDefRegistry()
	if err := agentDefs.Register(&agent.AgentDef{
		Name:        "general",
		Description: "General-purpose agent with all available tools",
	}); err != nil {
		logf("warning: registering general agent def: %v", err)
	}
	for _, defConf := range opts.Config.Agent.Definitions {
		if err := agentDefs.Register(&agent.AgentDef{
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
		}); err != nil {
			logf("warning: registering agent def %q: %v", defConf.Name, err)
		}
	}

	wakeManager := agent.NewWakeManager()
	spawner := &agent.DefaultSubagentSpawner{
		Config:    opts.Config,
		AgentDefs: agentDefs,
	}
	w := &Wiring{AgentDefs: agentDefs, WakeManager: wakeManager, Spawner: spawner}

	if !opts.EnableTask && !opts.EnableListTasks {
		return w, nil
	}

	if mgr := resolveWorktreeManager(opts); mgr != nil {
		spawner.WorktreeProvider = &worktreeProviderAdapter{mgr: mgr}
	}

	taskTool := tools.NewTaskTool(
		&spawnerAdapter{spawner: spawner},
		&agentDefLookupAdapter{reg: agentDefs},
		0,
	)
	taskTool.SetBackgroundManager(&wakeManagerAdapter{wm: wakeManager})
	if opts.EnableTask {
		if err := opts.Registry.Register(taskTool); err != nil {
			return nil, fmt.Errorf("registering task tool: %w", err)
		}
	}
	if opts.EnableListTasks {
		if err := opts.Registry.Register(tools.NewListTasksTool(&wakeStatusAdapter{wm: wakeManager})); err != nil {
			return nil, fmt.Errorf("registering list_tasks tool: %w", err)
		}
	}

	return w, nil
}

// resolveWorktreeManager prefers the session's manager and falls back to one
// rooted at the repository, so subagent isolation works even when the session
// is not itself in a worktree. Returns nil when there is no repository.
func resolveWorktreeManager(opts Options) *worktree.Manager {
	if opts.WorktreeManager != nil {
		return opts.WorktreeManager
	}
	if opts.GitRoot == nil {
		return nil
	}
	root, err := opts.GitRoot()
	if err != nil || root == "" {
		return nil
	}
	return worktree.NewManager(root, worktree.Config{
		MaxWorktrees: opts.Config.Worktree.MaxCount,
		BaseBranch:   opts.Config.Worktree.BaseBranch,
		AutoCleanup:  opts.Config.Worktree.AutoCleanup,
	})
}
