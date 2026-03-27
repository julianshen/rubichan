package shell

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// TurnEvent represents a streaming event from the agent.
type TurnEvent struct {
	Type     string // "text_delta", "tool_call", "tool_result", "done", "error"
	Text     string
	ToolName string // populated for "tool_call" events
}

// AgentTurnFunc executes a single agent turn and streams events.
type AgentTurnFunc func(ctx context.Context, userMessage string) (<-chan TurnEvent, error)

// ShellExecFunc executes a shell command and returns stdout, stderr, and exit code.
type ShellExecFunc func(ctx context.Context, command string, workDir string) (stdout string, stderr string, exitCode int, err error)

// SlashCommandFunc handles a slash command. Returns output text, whether to quit, and error.
type SlashCommandFunc func(ctx context.Context, name string, args []string) (output string, quit bool, err error)

// ErrExit is the sentinel error indicating normal exit from the shell.
var ErrExit = errors.New("exit")

// ShellHost runs the shell mode REPL loop.
type ShellHost struct {
	classifier      *InputClassifier
	history         *CommandHistory
	ctxTracker      *ContextTracker
	prompt          *PromptRenderer
	agentTurn       AgentTurnFunc
	shellExec       ShellExecFunc
	slashCommandFn  SlashCommandFunc
	workDir         string
	stdin           io.Reader
	stdout          io.Writer
	stderr          io.Writer
	gitBranchFn     func(string) string // returns git branch for a directory
}

// ShellHostConfig configures the shell host.
type ShellHostConfig struct {
	WorkDir        string
	HomeDir        string
	AgentTurn      AgentTurnFunc
	ShellExec      ShellExecFunc
	SlashCommandFn SlashCommandFunc
	Executables    map[string]bool
	MaxHistory     int
	Stdin          io.Reader
	Stdout         io.Writer
	Stderr         io.Writer
	GitBranchFn    func(string) string
}

// NewShellHost creates a new shell host with the given configuration.
func NewShellHost(cfg ShellHostConfig) *ShellHost {
	if cfg.MaxHistory == 0 {
		cfg.MaxHistory = 1000
	}
	if cfg.Stdin == nil {
		cfg.Stdin = os.Stdin
	}
	if cfg.Stdout == nil {
		cfg.Stdout = os.Stdout
	}
	if cfg.Stderr == nil {
		cfg.Stderr = os.Stderr
	}
	if cfg.GitBranchFn == nil {
		cfg.GitBranchFn = func(string) string { return "" }
	}

	return &ShellHost{
		classifier:     NewInputClassifier(cfg.Executables),
		history:        NewCommandHistory(cfg.MaxHistory),
		ctxTracker:     NewContextTracker(4096),
		prompt:         NewPromptRenderer(cfg.HomeDir),
		agentTurn:      cfg.AgentTurn,
		shellExec:      cfg.ShellExec,
		slashCommandFn: cfg.SlashCommandFn,
		workDir:        cfg.WorkDir,
		stdin:          cfg.Stdin,
		stdout:         cfg.Stdout,
		stderr:         cfg.Stderr,
		gitBranchFn:    cfg.GitBranchFn,
	}
}

// Mode returns the agent mode label for shell mode.
func (h *ShellHost) Mode() string {
	return "shell"
}

// Run starts the REPL loop. It blocks until EOF, exit, or context cancellation.
func (h *ShellHost) Run(ctx context.Context) error {
	scanner := bufio.NewScanner(h.stdin)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Render prompt
		branch := h.gitBranchFn(h.workDir)
		promptStr := h.prompt.Render(h.workDir, branch)
		fmt.Fprint(h.stdout, promptStr)

		// Read input
		if !scanner.Scan() {
			// EOF (Ctrl-D)
			fmt.Fprintln(h.stdout)
			return nil
		}

		line := scanner.Text()
		input := h.classifier.Classify(line)

		switch input.Classification {
		case ClassEmpty:
			continue

		case ClassBuiltinCommand:
			if err := h.handleBuiltin(input); err != nil {
				return err
			}

		case ClassShellCommand:
			h.handleShellCommand(ctx, input)

		case ClassLLMQuery:
			h.handleLLMQuery(ctx, line)

		case ClassSlashCommand:
			if quit := h.handleSlashCommand(ctx, input); quit {
				return ErrExit
			}
		}
	}
}

func (h *ShellHost) handleBuiltin(input ClassifiedInput) error {
	switch input.Command {
	case "exit", "quit":
		return ErrExit
	case "cd":
		return h.handleCD(input.Args)
	}
	return nil
}

func (h *ShellHost) handleCD(args []string) error {
	if len(args) == 0 {
		return nil
	}
	target := args[0]

	// Resolve relative to current workDir
	if !filepath.IsAbs(target) {
		target = filepath.Join(h.workDir, target)
	}
	target = filepath.Clean(target)

	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		fmt.Fprintf(h.stderr, "cd: no such directory: %s\n", args[0])
		return nil
	}

	h.workDir = target
	return nil
}

func (h *ShellHost) handleShellCommand(ctx context.Context, input ClassifiedInput) {
	h.history.Add(input.Command)

	if h.shellExec == nil {
		fmt.Fprintf(h.stderr, "shell execution not available\n")
		return
	}

	stdout, stderr, exitCode, err := h.shellExec(ctx, input.Command, h.workDir)
	if err != nil {
		fmt.Fprintf(h.stderr, "error: %v\n", err)
		return
	}

	if stdout != "" {
		fmt.Fprint(h.stdout, stdout)
		if !strings.HasSuffix(stdout, "\n") {
			fmt.Fprintln(h.stdout)
		}
	}
	if stderr != "" {
		fmt.Fprint(h.stderr, stderr)
		if !strings.HasSuffix(stderr, "\n") {
			fmt.Fprintln(h.stderr)
		}
	}

	// Record in context tracker for potential LLM follow-up
	combined := stdout
	if stderr != "" {
		combined += stderr
	}
	h.ctxTracker.Record(input.Command, combined, exitCode)
}

func (h *ShellHost) handleLLMQuery(ctx context.Context, query string) {
	h.history.Add(query)

	if h.agentTurn == nil {
		fmt.Fprintf(h.stderr, "agent not available\n")
		return
	}

	// Build the message with optional context from last shell command
	message := query
	ctxMsg := h.ctxTracker.ContextMessage()
	if ctxMsg != "" {
		message = ctxMsg + "\n\nUser query: " + query
		h.ctxTracker.Clear()
	}

	events, err := h.agentTurn(ctx, message)
	if err != nil {
		fmt.Fprintf(h.stderr, "error: %v\n", err)
		return
	}

	for event := range events {
		switch event.Type {
		case "text_delta":
			fmt.Fprint(h.stdout, event.Text)
		case "tool_call":
			fmt.Fprintf(h.stderr, "[Running: %s]\n", event.ToolName)
		case "tool_result":
			if event.Text != "" {
				fmt.Fprintf(h.stderr, "[Result: %s]\n", truncateForDisplay(event.Text, 200))
			}
		case "done":
			fmt.Fprintln(h.stdout)
		case "error":
			fmt.Fprintf(h.stderr, "error: %s\n", event.Text)
		}
	}
}

func (h *ShellHost) handleSlashCommand(ctx context.Context, input ClassifiedInput) (quit bool) {
	if h.slashCommandFn == nil {
		fmt.Fprintf(h.stderr, "command not available: /%s\n", input.Command)
		return false
	}

	output, quit, err := h.slashCommandFn(ctx, input.Command, input.Args)
	if err != nil {
		fmt.Fprintf(h.stderr, "error: %v\n", err)
		return false
	}
	if output != "" {
		fmt.Fprintln(h.stdout, output)
	}
	return quit
}

// truncateForDisplay truncates a string to maxLen, adding ellipsis if needed.
func truncateForDisplay(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
