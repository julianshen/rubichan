package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/commands"
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

	// Set up effective working directory (creates worktree if --worktree is set).
	cwd, _, wtCleanup, err := setupWorkingDir(cfg)
	if err != nil {
		return fmt.Errorf("worktree setup: %w", err)
	}
	defer wtCleanup()

	// Create provider.
	p, err := provider.NewProvider(cfg)
	if err != nil {
		return fmt.Errorf("creating provider: %w", err)
	}

	// Create tool registry.
	registry := tools.NewRegistry()
	diffTracker := tools.NewDiffTracker()
	toolsCfg := ToolsConfig{
		ModelCapabilities: provider.DetectCapabilities(cfg.Provider.Default, cfg.Provider.Model),
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
	shellCaps := provider.DetectCapabilities(cfg.Provider.Default, cfg.Provider.Model)
	opts = append(opts, agent.WithCapabilities(shellCaps))

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
		approvalFunc = makeShellApprovalFunc(os.Stderr)
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

	// Create command registry and register built-in slash commands.
	cmdRegistry := commands.NewRegistry()
	for _, cmd := range []commands.SlashCommand{
		commands.NewQuitCommand(),
		commands.NewExitCommand(),
		commands.NewHelpCommand(cmdRegistry),
	} {
		if err := cmdRegistry.Register(cmd); err != nil {
			return fmt.Errorf("register built-in command %q: %w", cmd.Name(), err)
		}
	}

	// Wire command registry into skill runtime.
	if rt != nil {
		rt.SetCommandRegistry(cmdRegistry)
	}

	// Create agent.
	a := agent.New(p, registry, approvalFunc, cfg, opts...)

	// Register model command (needs the agent instance).
	if err := cmdRegistry.Register(commands.NewModelCommand(func(name string) {
		a.SetModel(name)
	})); err != nil {
		return fmt.Errorf("register built-in command %q: %w", "model", err)
	}

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

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not determine home directory: %v\n", err)
	}

	host := shell.NewShellHost(shell.ShellHostConfig{
		WorkDir:        cwd,
		HomeDir:        homeDir,
		AgentTurn:      makeAgentTurnFunc(a),
		ShellExec:      makeShellExecFunc(registry),
		SlashCommandFn: makeSlashCommandFunc(cmdRegistry),
		Executables:    executables,
		Stdin:          os.Stdin,
		Stdout:         os.Stdout,
		Stderr:         os.Stderr,
		GitBranchFn:    gitBranchFn,
	})

	err = host.Run(ctx)
	if errors.Is(err, shell.ErrExit) {
		return nil
	}
	return err
}

// makeShellApprovalFunc creates an inline yes/no approval prompt for shell mode.
// It writes prompts to the given writer (stderr) to avoid conflicting with
// the shell host's stdin scanner.
func makeShellApprovalFunc(promptWriter *os.File) agent.ApprovalFunc {
	return func(_ context.Context, toolName string, _ json.RawMessage) (bool, error) {
		fmt.Fprintf(promptWriter, "[Approve %s?] (y/n): ", toolName)
		buf := make([]byte, 64)
		n, err := promptWriter.Read(buf) // read from tty via stderr's fd
		if err != nil {
			// Fallback: read from /dev/tty directly
			tty, ttyErr := os.Open("/dev/tty")
			if ttyErr != nil {
				return false, fmt.Errorf("no terminal available for approval prompt: %w", ttyErr)
			}
			defer tty.Close()
			n, err = tty.Read(buf)
			if err != nil {
				return false, fmt.Errorf("reading approval response: %w", err)
			}
		}
		response := strings.TrimSpace(strings.ToLower(string(buf[:n])))
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
				case "tool_call":
					toolName := ""
					if event.ToolCall != nil {
						toolName = event.ToolCall.Name
					}
					ch <- shell.TurnEvent{Type: "tool_call", ToolName: toolName}
				case "tool_result":
					text := ""
					if event.ToolResult != nil {
						text = event.ToolResult.Content
					}
					ch <- shell.TurnEvent{Type: "tool_result", Text: text}
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

// makeSlashCommandFunc wraps the commands.Registry into a shell.SlashCommandFunc.
func makeSlashCommandFunc(registry *commands.Registry) shell.SlashCommandFunc {
	return func(ctx context.Context, name string, args []string) (string, bool, error) {
		cmd, ok := registry.Get(name)
		if !ok {
			return fmt.Sprintf("unknown command: /%s", name), false, nil
		}

		result, err := cmd.Execute(ctx, args)
		if err != nil {
			return "", false, err
		}

		quit := result.Action == commands.ActionQuit
		return result.Output, quit, nil
	}
}

// shellConfigDir returns the path for shell-mode-specific config like history.
func shellConfigDir() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	shellDir := filepath.Join(dir, "shell")
	if err := os.MkdirAll(shellDir, 0o755); err != nil {
		return "", err
	}
	return shellDir, nil
}
