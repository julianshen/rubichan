package shell

import (
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
	Type     string // EventTextDelta, EventToolCall, EventToolResult, EventDone, EventError
	Text     string
	ToolName string // populated for EventToolCall events
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
	classifier       *InputClassifier
	history          *CommandHistory
	ctxTracker       *ContextTracker
	prompt           *PromptRenderer
	agentTurn        AgentTurnFunc
	shellExec        ShellExecFunc
	slashCommandFn   SlashCommandFunc
	errorAnalyzer    *ErrorAnalyzer
	scriptGen        *ScriptGenerator
	intentClassifier *IntentClassifier
	scriptApprovalFn func(ctx context.Context, script string) (bool, string, error)
	pkgInstaller     *PackageInstaller
	statusLine       *StatusLine
	lineReader       LineReader
	workDir          string
	stdin            io.Reader
	stdout           io.Writer
	stderr           io.Writer
	gitBranchFn      func(string) string
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
	ErrorAnalysis     bool
	ScriptApprovalFn  func(ctx context.Context, script string) (bool, string, error)
	PackageManager    *PackageManager
	InstallApprovalFn func(ctx context.Context, action string) (bool, error)
	StatusLine        bool
	LineReader        LineReader
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

	var ea *ErrorAnalyzer
	if cfg.ErrorAnalysis && cfg.AgentTurn != nil {
		ea = NewErrorAnalyzer(cfg.AgentTurn, 4096)
	}

	var pi *PackageInstaller
	if cfg.PackageManager != nil {
		pi = NewPackageInstaller(cfg.PackageManager, cfg.AgentTurn, cfg.ShellExec, cfg.InstallApprovalFn)
	}

	pr := NewPromptRenderer(cfg.HomeDir)

	var sl *StatusLine
	if cfg.StatusLine {
		sl = NewStatusLine(80)
		sl.homeDir = cfg.HomeDir
		sl.UpdateCWD(cfg.WorkDir)
		pr.statusLine = sl
	}

	// When no LineReader is provided, wrap Stdin in a SimpleLineReader
	lr := cfg.LineReader
	if lr == nil {
		lr = NewSimpleLineReader(cfg.Stdin)
	}

	h := &ShellHost{
		classifier:       NewInputClassifier(cfg.Executables),
		history:          NewCommandHistory(cfg.MaxHistory),
		ctxTracker:       NewContextTracker(4096),
		prompt:           pr,
		agentTurn:        cfg.AgentTurn,
		shellExec:        cfg.ShellExec,
		slashCommandFn:   cfg.SlashCommandFn,
		errorAnalyzer:    ea,
		scriptApprovalFn: cfg.ScriptApprovalFn,
		pkgInstaller:     pi,
		statusLine:       sl,
		lineReader:       lr,
		workDir:          cfg.WorkDir,
		stdin:            cfg.Stdin,
		stdout:           cfg.Stdout,
		stderr:           cfg.Stderr,
		gitBranchFn:      cfg.GitBranchFn,
	}

	if cfg.ScriptApprovalFn != nil && cfg.AgentTurn != nil {
		h.intentClassifier = NewIntentClassifier(cfg.AgentTurn)
		h.scriptGen = NewScriptGenerator(cfg.AgentTurn, cfg.ShellExec, &h.workDir)
	}

	return h
}

// Mode returns the agent mode label for shell mode.
func (h *ShellHost) Mode() string {
	return "shell"
}

// Run starts the REPL loop. It blocks until EOF, exit, or context cancellation.
func (h *ShellHost) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		branch := h.gitBranchFn(h.workDir)
		if h.statusLine != nil {
			h.statusLine.Update(SegmentBranch, branch)
		}
		promptStr := h.prompt.Render(h.workDir, branch)
		fmt.Fprint(h.stdout, promptStr)

		line, err := h.lineReader.ReadLine(promptStr)
		if err != nil {
			if err == io.EOF {
				fmt.Fprintln(h.stdout)
				return nil
			}
			return fmt.Errorf("reading shell input: %w", err)
		}

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
			h.handleLLMQuery(ctx, input)
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

	if h.statusLine != nil {
		h.statusLine.UpdateCWD(target)
		branch := h.gitBranchFn(target)
		h.statusLine.Update(SegmentBranch, branch)
	}

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

	writeOutput(h.stdout, stdout)
	writeOutput(h.stderr, stderr)

	combined := stdout
	if stderr != "" {
		combined += stderr
	}
	h.ctxTracker.Record(input.Command, combined, exitCode)

	if h.statusLine != nil {
		h.statusLine.UpdateExitCode(exitCode)
		if strings.HasPrefix(input.Command, "git ") {
			branch := h.gitBranchFn(h.workDir)
			h.statusLine.Update(SegmentBranch, branch)
		}
	}

	if exitCode != 0 && h.pkgInstaller != nil {
		handled, _ := h.pkgInstaller.HandleCommandNotFound(ctx, input.Command, stderr, exitCode, h.stdout, h.stderr)
		if handled {
			return
		}
	}

	if exitCode != 0 && h.errorAnalyzer != nil {
		fmt.Fprintf(h.stderr, "\n💡 Analyzing error...\n")
		events, err := h.errorAnalyzer.Analyze(ctx, input.Command, stdout, stderr, exitCode)
		if err == nil && events != nil {
			for event := range events {
				switch event.Type {
				case EventTextDelta:
					fmt.Fprint(h.stdout, event.Text)
				case EventDone:
					fmt.Fprintln(h.stdout)
				case EventError:
					fmt.Fprintf(h.stderr, "analysis error: %s\n", event.Text)
				}
			}
		}
	}
}

func (h *ShellHost) handleLLMQuery(ctx context.Context, input ClassifiedInput) {
	query := strings.TrimSpace(input.Raw)
	isForceQuery := strings.HasPrefix(query, "?")
	if isForceQuery {
		query = strings.TrimSpace(query[1:])
	}

	h.history.Add(query)

	if h.agentTurn == nil {
		fmt.Fprintf(h.stderr, "agent not available\n")
		return
	}

	// Smart script: for ?-prefixed input, classify intent and potentially generate a script
	if isForceQuery && h.scriptGen != nil && h.intentClassifier != nil {
		intent, _ := h.intentClassifier.Classify(ctx, query)
		if intent == IntentAction {
			h.handleSmartScript(ctx, query)
			return
		}
	}

	message := query
	ctxMsg := h.ctxTracker.ContextMessage()
	if ctxMsg != "" {
		message = ctxMsg + "\n\nUser query: " + query
	}

	events, err := h.agentTurn(ctx, message)
	if err != nil {
		fmt.Fprintf(h.stderr, "error: %v\n", err)
		return
	}

	if ctxMsg != "" {
		h.ctxTracker.Clear()
	}

	h.streamEvents(events)
}

func (h *ShellHost) handleSmartScript(ctx context.Context, query string) {
	script, err := h.scriptGen.Generate(ctx, query)
	if err != nil {
		fmt.Fprintf(h.stderr, "error generating script: %v\n", err)
		return
	}

	fmt.Fprintf(h.stdout, "\nGenerated script:\n```\n%s\n```\n\n", script)

	approved, editedScript, err := h.scriptApprovalFn(ctx, script)
	if err != nil {
		fmt.Fprintf(h.stderr, "error: %v\n", err)
		return
	}

	if !approved {
		fmt.Fprintln(h.stderr, "Script discarded.")
		return
	}

	runScript := script
	if editedScript != "" {
		runScript = editedScript
	}

	stdout, stderr, exitCode, err := h.scriptGen.Execute(ctx, runScript)
	if err != nil {
		fmt.Fprintf(h.stderr, "error: %v\n", err)
		return
	}

	writeOutput(h.stdout, stdout)
	writeOutput(h.stderr, stderr)

	combined := stdout
	if stderr != "" {
		combined += stderr
	}
	h.ctxTracker.Record(runScript, combined, exitCode)
}

func (h *ShellHost) streamEvents(events <-chan TurnEvent) {
	for event := range events {
		switch event.Type {
		case EventTextDelta:
			fmt.Fprint(h.stdout, event.Text)
		case EventToolCall:
			fmt.Fprintf(h.stderr, "[Running: %s]\n", event.ToolName)
		case EventToolResult:
			if event.Text != "" {
				fmt.Fprintf(h.stderr, "[Result: %s]\n", truncateForDisplay(event.Text, 200))
			}
		case EventDone:
			fmt.Fprintln(h.stdout)
		case EventError:
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
