package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/folderaccess"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/internal/tools/xcode"
)

// checkACPToolConsent refuses to serve ACP without an explicit decision about
// tool approval.
//
// ACP's mechanism for this is session/request_permission, which this agent does
// not implement: Conn.Request can express the round trip, but nothing calls it.
// So a served agent cannot ask the user before running a shell command.
//
// Auto-approving on the grounds that the mode is non-interactive would hand
// whatever spawned the process arbitrary command execution on the user's
// machine with nobody having agreed to it. Headless can default to
// auto-approval because a person typed the command that started it; here the
// caller is an editor, and the person may not know a tool ran at all.
//
// --approve-cwd deliberately does NOT satisfy this. It governs folder access —
// whether the agent may work in this directory at all — and grants no scope
// over which tools may run unattended once it does. Accepting it here, as an
// earlier version did, promised a restriction that nothing implemented.
func checkACPToolConsent() error {
	if autoApprove {
		return nil
	}
	return fmt.Errorf(
		"--acp cannot ask for tool approval: this agent does not implement ACP's session/request_permission, " +
			"so a client cannot be prompted before a tool runs. Re-run with --auto-approve to accept unattended " +
			"tool execution. (--approve-cwd does not substitute: it grants folder access, not a limit on which " +
			"tools may run.)")
}

// runACP serves the Agent Client Protocol over stdin/stdout until the client
// disconnects.
//
// stdio is the transport ACP clients use: the editor spawns this process and
// speaks JSON-RPC down the pipe. That makes stdout part of the protocol, so
// nothing else may write to it — diagnostics go to stderr, which is where Go's
// log package already sends them.
func runACP() error {
	if err := checkACPToolConsent(); err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if maxTurnsFlag > 0 {
		cfg.Agent.MaxTurns = maxTurnsFlag
	}

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

	// The approval posture the consent check above already forced the caller to
	// choose. Hierarchical deny policies still apply, so an org-level
	// restriction is not lost by opting in.
	var checkers []agent.ApprovalChecker
	if hc := buildHierarchicalChecker(cfg, configPath, cwd); hc != nil {
		checkers = append(checkers, hc)
	}
	checkers = append(checkers, agent.AlwaysAutoApprove{})
	composite := agent.NewCompositeApprovalChecker(checkers...)

	approvalFunc := func(context.Context, string, json.RawMessage) (bool, error) {
		return true, nil
	}

	a := agent.New(p, registry, approvalFunc, cfg,
		agent.WithWorkingDir(cwd),
		agent.WithCapabilities(modelCaps),
		agent.WithApprovalChecker(composite),
	)

	// Background, not a timeout: an ACP connection lives as long as the client
	// keeps it open. --timeout governs a single headless run and would cut a
	// live editor session off mid-conversation.
	return agent.ServeACP(context.Background(), a, os.Stdin, os.Stdout)
}
