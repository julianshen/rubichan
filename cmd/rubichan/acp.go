package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/julianshen/rubichan/internal/agent"
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
func checkACPToolConsent() error {
	if autoApprove || approveCwd {
		return nil
	}
	return fmt.Errorf(
		"--acp cannot ask for tool approval: this agent does not implement ACP's session/request_permission, " +
			"so a client cannot be prompted before a tool runs. Re-run with --auto-approve to accept unattended " +
			"tool execution, or --approve-cwd to limit it to the working directory")
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

	p, err := provider.NewProviderWithDebug(cfg, debugMode)
	if err != nil {
		return fmt.Errorf("creating provider: %w", err)
	}

	registry := tools.NewRegistry()
	toolsCfg := ToolsConfig{
		ModelCapabilities: provider.DetectCapabilities(cfg.Provider.Default, cfg.Provider.Model),
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
		agent.WithApprovalChecker(composite),
	)

	// Background, not a timeout: an ACP connection lives as long as the client
	// keeps it open. --timeout governs a single headless run and would cut a
	// live editor session off mid-conversation.
	return agent.ServeACP(context.Background(), a, os.Stdin, os.Stdout)
}
