package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/folderaccess"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/internal/tools/xcode"
)

// acpApprovalCheckers decides which tool calls skip the user entirely.
//
// A checker that answers AutoApproved short-circuits the approval flow before
// the client is consulted, so AlwaysAutoApprove is added only when the user
// asked for unattended execution. Adding it unconditionally — as this mode did
// while it could not ask at all — would leave a permission prompt in the code
// that no user ever sees.
//
// --approve-cwd is deliberately not consulted. It governs folder access,
// whether the agent may work in this directory at all, and grants no scope over
// which tools may run unattended once it does.
//
// Hierarchical deny policies come first either way, so an org-level restriction
// survives an opt-in.
func acpApprovalCheckers(cfg *config.Config, cwd string) []agent.ApprovalChecker {
	var checkers []agent.ApprovalChecker
	if hc := buildHierarchicalChecker(cfg, configPath, cwd); hc != nil {
		checkers = append(checkers, hc)
	}
	if autoApprove {
		checkers = append(checkers, agent.AlwaysAutoApprove{})
	}
	return checkers
}

// acpApprovalWithoutAClient is the backstop the agent falls back to when no UI
// handler is installed.
//
// In this mode one always is, so reaching here means a wiring mistake rather
// than a user decision — and answering "approved" would turn that mistake into
// arbitrary command execution that nobody agreed to. Denying makes the mistake
// visible in a tool result instead.
func acpApprovalWithoutAClient(_ context.Context, tool string, _ json.RawMessage) (bool, error) {
	return false, fmt.Errorf(
		"acp: tool approval was not routed to the client, so nobody could be asked about %q", tool)
}

// runACP serves the Agent Client Protocol over stdin/stdout until the client
// disconnects.
//
// stdio is the transport ACP clients use: the editor spawns this process and
// speaks JSON-RPC down the pipe. That makes stdout part of the protocol, so
// nothing else may write to it — diagnostics go to stderr, which is where Go's
// log package already sends them.
func runACP() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if maxTurnsFlag > 0 {
		cfg.Agent.MaxTurns = maxTurnsFlag
	}

	// Signals are the only cancellation path this mode has. ACP's session/cancel
	// is unimplementable while Handler carries no context, and the drain now
	// waits without a deadline — so without this, a turn wedged in a provider
	// call after the editor disconnects would keep the process alive forever.
	// An editor that terminates its agent subprocess sends one of these.
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	// The same folder-access gate interactive and headless apply. Without it
	// ACP would be the one mode that works in a directory the user never
	// approved — and it is the mode where the user is least likely to notice,
	// because an editor started it.
	cfgDir, err := configDir()
	if err != nil {
		return fmt.Errorf("resolving config directory: %w", err)
	}
	st, err := openStore(cfgDir)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}
	defer st.Close()
	if err := folderaccess.EnsureApprovedNonInteractive(st, cwd, autoApprove, approveCwd); err != nil {
		return err
	}

	p, err := provider.NewProviderWithDebug(cfg, debugMode)
	if err != nil {
		return fmt.Errorf("creating provider: %w", err)
	}

	// Detected once and used twice: tool registration reads it, and the agent
	// needs it too. Interactive, headless and shell all pass it through
	// WithCapabilities; ACP dropping it would silently ignore tool-count
	// limits, discovery hints and configured reasoning effort.
	modelCaps := provider.DetectCapabilities(cfg.Provider.Default, cfg.Provider.Model)

	registry := tools.NewRegistry()
	toolsCfg := ToolsConfig{
		ModelCapabilities: modelCaps,
		ProjectContext: ProjectContext{
			AppleProjectDetected: xcode.DiscoverProject(cwd).Type != "none",
			AppleSkillRequested:  containsSkill("apple-dev", skillsFlag),
		},
		CLIOverrides: parseToolsFlag(toolsFlag),
	}
	coreTools, err := registerCoreTools(cwd, registry, cfg, toolsCfg, tools.NewDiffTracker(), timeoutFlag)
	if err != nil {
		return fmt.Errorf("registering tools: %w", err)
	}
	for _, cleanup := range coreTools.cleanups {
		defer cleanup()
	}

	// Every other mode wires these; ACP skipping them made it the odd one out
	// in ways a user would only discover by their absence.
	if err := wireAppleDev(cwd, registry, toolsCfg); err != nil {
		return fmt.Errorf("wiring apple dev tools: %w", err)
	}

	// --skills was read for the apple-dev check above and otherwise ignored, so
	// a client launching with --skills got none of them.
	rt, skillCloser, err := createSkillRuntime(ctx, registry, p, cfg, "acp", cwd, cfgDir)
	if err != nil {
		return fmt.Errorf("creating skill runtime: %w", err)
	}
	if skillCloser != nil {
		defer skillCloser.Close()
	}

	composite := agent.NewCompositeApprovalChecker(acpApprovalCheckers(cfg, cwd)...)

	// Tool approval goes to the editor: the agent asks over the same connection
	// it is streaming the turn on, and the user answers in their client. This is
	// what lifted the --auto-approve requirement this mode used to carry — it
	// could not ask anyone, so the only alternative to refusing to start was
	// running everything unattended.
	perms := agent.NewACPPermissions()

	// The tool security pipeline. Interactive and headless both install it, and
	// ACP omitting it meant .security.yaml and Agent.ToolRules denials were not
	// enforced. It matters most under --auto-approve, where the rule engine is
	// the only thing left saying no.
	pipeline := buildPipeline(registry, cfg, cwd, rt)

	a := agent.New(p, registry, acpApprovalWithoutAClient, cfg,
		agent.WithWorkingDir(cwd),
		agent.WithCapabilities(modelCaps),
		agent.WithApprovalChecker(composite),
		agent.WithUIRequestHandler(perms),
		agent.WithSkillRuntime(rt),
		agent.WithToolMiddlewares(pipeline.Middlewares),
	)

	// Signal-cancellable, not a timeout: an ACP connection lives as long as the
	// client keeps it open, and --timeout governs a single headless run, so it
	// would cut a live editor session off mid-conversation.
	return agent.ServeACP(ctx, a, os.Stdin, os.Stdout, perms)
}
