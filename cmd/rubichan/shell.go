package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/shell"
	"github.com/julianshen/rubichan/internal/tools"
)

func shellCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shell",
		Short: "Start AI-enhanced interactive shell",
		Long: `Shell mode provides an AI-enhanced interactive shell where natural
language and shell commands coexist in one prompt. Commands are executed
directly with zero LLM overhead, while natural language queries are
routed to the agent.

Prefix input with ! to force shell execution, or ? to force LLM query.
Known executables from $PATH are auto-detected and run directly.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runShell()
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	return cmd
}

func runShell() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	// Create provider.
	p, err := provider.NewProvider(cfg)
	if err != nil {
		return fmt.Errorf("creating provider: %w", err)
	}

	// Create tool registry.
	registry := tools.NewRegistry()
	diffTracker := tools.NewDiffTracker()
	toolsCfg := ToolsConfig{
		ModelCapabilities: detectModelCapabilities(cfg),
	}
	coreResult, err := registerCoreTools(cwd, registry, cfg, toolsCfg, diffTracker, 120*time.Second)
	if err != nil {
		return err
	}
	for _, cleanup := range coreResult.cleanups {
		defer cleanup()
	}

	// Build agent options.
	var opts []agent.AgentOption
	opts = append(opts, agent.WithDiffTracker(diffTracker))
	opts = appendWorkingDirOption(opts, cwd)
	opts = appendPersonaOptions(opts, cwd)
	opts = append(opts, agent.WithMode("shell"))

	// Handle --resume flag.
	if resumeFlag != "" {
		opts = append(opts, agent.WithResumeSession(resumeFlag))
	}

	// Build approval function.
	var approvalFunc agent.ApprovalFunc
	if autoApprove {
		approvalFunc = func(_ context.Context, _ string, _ json.RawMessage) (bool, error) {
			return true, nil
		}
		opts = append(opts, agent.WithApprovalChecker(
			agent.NewCompositeApprovalChecker(agent.AlwaysAutoApprove{}),
		))
	} else {
		approvalFunc = makeShellApprovalFunc()
	}

	// Create skill runtime.
	ctx := context.Background()
	rt, storeCloser, err := createSkillRuntime(ctx, registry, p, cfg, "shell", cwd)
	if err != nil {
		return fmt.Errorf("creating skill runtime: %w", err)
	}
	if storeCloser != nil {
		defer storeCloser.Close()
	}
	if rt != nil {
		opts = append(opts, agent.WithSkillRuntime(rt))
	}

	// Create agent.
	a := agent.New(p, registry, approvalFunc, cfg, opts...)

	// Scan PATH for known executables.
	executables := shell.ScanPATH()

	// Build git branch function.
	gitBranchFn := func(dir string) string {
		branch, err := detectGitBranch(dir)
		if err != nil {
			return ""
		}
		return branch
	}

	// Build shell exec function using the registry's shell tool.
	shellExec := makeShellExecFunc(registry)

	// Build agent turn function.
	agentTurn := makeAgentTurnFunc(a)

	homeDir, _ := os.UserHomeDir()

	host := shell.NewShellHost(shell.ShellHostConfig{
		WorkDir:     cwd,
		HomeDir:     homeDir,
		AgentTurn:   agentTurn,
		ShellExec:   shellExec,
		Executables: executables,
		Stdin:       os.Stdin,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		GitBranchFn: gitBranchFn,
	})

	err = host.Run(ctx)
	if err != nil && err.Error() == "exit" {
		return nil // Normal exit
	}
	return err
}

// makeShellApprovalFunc creates an inline yes/no approval prompt for shell mode.
func makeShellApprovalFunc() agent.ApprovalFunc {
	return func(_ context.Context, toolName string, _ json.RawMessage) (bool, error) {
		fmt.Fprintf(os.Stderr, "[Approve %s?] (y/n): ", toolName)
		var response string
		if _, err := fmt.Scanln(&response); err != nil {
			return false, nil
		}
		response = strings.TrimSpace(strings.ToLower(response))
		return response == "y" || response == "yes", nil
	}
}

// makeShellExecFunc creates a ShellExecFunc that finds the shell tool from the registry.
func makeShellExecFunc(registry *tools.Registry) shell.ShellExecFunc {
	return func(ctx context.Context, command string, workDir string) (string, string, int, error) {
		shellTool, ok := registry.Get("shell")
		if !ok {
			return "", "", 1, fmt.Errorf("shell tool not registered")
		}

		input := map[string]interface{}{
			"command":     command,
			"directory":   workDir,
			"description": "shell mode command",
		}
		inputJSON, err := json.Marshal(input)
		if err != nil {
			return "", "", 1, fmt.Errorf("marshaling input: %w", err)
		}

		result, err := shellTool.Execute(ctx, inputJSON)
		if err != nil {
			return "", "", 1, err
		}

		exitCode := 0
		if result.IsError {
			exitCode = 1
		}
		return result.Display(), "", exitCode, nil
	}
}

// makeAgentTurnFunc wraps agent.Turn into a shell.AgentTurnFunc.
func makeAgentTurnFunc(a *agent.Agent) shell.AgentTurnFunc {
	return func(ctx context.Context, userMessage string) (<-chan shell.TurnEvent, error) {
		agentEvents, err := a.Turn(ctx, userMessage)
		if err != nil {
			return nil, err
		}

		ch := make(chan shell.TurnEvent, 16)
		go func() {
			defer close(ch)
			for event := range agentEvents {
				switch event.Type {
				case "text_delta":
					ch <- shell.TurnEvent{Type: "text_delta", Text: event.Text}
				case "done":
					ch <- shell.TurnEvent{Type: "done"}
				case "error":
					errText := ""
					if event.Error != nil {
						errText = event.Error.Error()
					}
					ch <- shell.TurnEvent{Type: "error", Text: errText}
				}
			}
		}()

		return ch, nil
	}
}
