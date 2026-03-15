package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/internal/persona"
	"github.com/julianshen/rubichan/internal/session"
	"github.com/julianshen/rubichan/internal/skills"
)

type plainInteractiveHost struct {
	agent          *agent.Agent
	cmdRegistry    *commands.Registry
	reader         *bufio.Reader
	out            io.Writer
	modelName      string
	maxTurns       int
	turnCount      int
	totalCost      float64
	gitBranch      string
	skillProvider  plainSkillSummaryProvider
	activeSkills   []string
	alwaysApproved map[string]bool
	alwaysDenied   map[string]bool
	sessionState   *session.State
	eventSink      session.EventSink
	mu             sync.RWMutex
}

type plainSkillSummaryProvider interface {
	GetAllSkillSummaries() []skills.SkillSummary
}

func newPlainInteractiveHost(in io.Reader, out io.Writer, modelName string, maxTurns int, cmdRegistry *commands.Registry) *plainInteractiveHost {
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	if cmdRegistry == nil {
		cmdRegistry = commands.NewRegistry()
	}
	return &plainInteractiveHost{
		cmdRegistry:    cmdRegistry,
		reader:         bufio.NewReader(in),
		out:            out,
		modelName:      modelName,
		maxTurns:       maxTurns,
		alwaysApproved: make(map[string]bool),
		alwaysDenied:   make(map[string]bool),
		sessionState:   session.NewState(),
		eventSink:      session.NewLogSink(log.Printf),
	}
}

func (h *plainInteractiveHost) SetAgent(a *agent.Agent) {
	h.agent = a
}

func (h *plainInteractiveHost) SetModel(name string) {
	h.modelName = name
}

func (h *plainInteractiveHost) SetGitBranch(branch string) {
	h.gitBranch = branch
}

func (h *plainInteractiveHost) SetSkillRuntime(rt plainSkillSummaryProvider) {
	h.skillProvider = rt
	h.refreshActiveSkills()
}

func (h *plainInteractiveHost) refreshActiveSkills() {
	if h.skillProvider == nil {
		h.activeSkills = nil
		return
	}
	summaries := h.skillProvider.GetAllSkillSummaries()
	active := make([]string, 0, len(summaries))
	for _, s := range summaries {
		if s.State == skills.SkillStateActive {
			active = append(active, s.Name)
		}
	}
	h.activeSkills = active
}

func (h *plainInteractiveHost) MakeApprovalFunc() agent.ApprovalFunc {
	return func(ctx context.Context, tool string, input json.RawMessage) (bool, error) {
		destructive := isDestructiveToolCall(tool, input)
		h.mu.RLock()
		if h.alwaysDenied[tool] {
			h.mu.RUnlock()
			return false, nil
		}
		if !destructive && h.alwaysApproved[tool] {
			h.mu.RUnlock()
			return true, nil
		}
		h.mu.RUnlock()

		fmt.Fprintf(h.out, "\nApproval required for tool %q\n", tool)
		if trimmed := strings.TrimSpace(string(input)); trimmed != "" {
			fmt.Fprintf(h.out, "Input: %s\n", trimmed)
		}
		fmt.Fprint(h.out, "[y]es / [n]o / [a]lways / [d]eny always: ")
		line, err := h.readLineCtx(ctx)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "a", "always":
			if !destructive {
				h.mu.Lock()
				h.alwaysApproved[tool] = true
				delete(h.alwaysDenied, tool)
				h.mu.Unlock()
			}
			return true, nil
		case "d", "deny", "deny-always":
			h.mu.Lock()
			h.alwaysDenied[tool] = true
			delete(h.alwaysApproved, tool)
			h.mu.Unlock()
			return false, nil
		case "y", "yes", "":
			return true, nil
		default:
			return false, nil
		}
	}
}

func (h *plainInteractiveHost) CheckApproval(tool string, input json.RawMessage) agent.ApprovalResult {
	if isDestructiveToolCall(tool, input) {
		return agent.ApprovalRequired
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.alwaysDenied[tool] {
		return agent.AutoDenied
	}
	if h.alwaysApproved[tool] {
		return agent.AutoApproved
	}
	return agent.ApprovalRequired
}

func (h *plainInteractiveHost) Run(ctx context.Context) error {
	h.printSessionHeader()
	for {
		select {
		case <-ctx.Done():
			return interactiveExitError(ctx)
		default:
		}

		if _, err := fmt.Fprint(h.out, "❯ "); err != nil {
			return err
		}
		line, err := h.readLineCtx(ctx)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		text := strings.TrimSpace(line)
		if text == "" {
			continue
		}
		if strings.HasPrefix(text, "/") {
			shouldQuit, err := h.handleCommand(text)
			if err != nil {
				return err
			}
			if shouldQuit {
				return nil
			}
			continue
		}
		if err := h.runTurn(ctx, text); err != nil {
			return err
		}
	}
}

func (h *plainInteractiveHost) handleCommand(line string) (bool, error) {
	parts, err := commands.ParseLine(line)
	if err != nil {
		_, _ = fmt.Fprintln(h.out, persona.ErrorMessage(err.Error()))
		return false, nil
	}
	if len(parts) == 0 {
		return false, nil
	}
	name := strings.ToLower(strings.TrimPrefix(parts[0], "/"))
	if name == "" {
		name = "help"
	}
	cmd, ok := h.cmdRegistry.Get(name)
	if !ok {
		_, _ = fmt.Fprintf(h.out, "Unknown command: %s\n", parts[0])
		return false, nil
	}

	result, err := cmd.Execute(context.Background(), parts[1:])
	if err != nil {
		_, _ = fmt.Fprintln(h.out, persona.ErrorMessage(err.Error()))
		return false, nil
	}

	prevActive := append([]string(nil), h.activeSkills...)
	h.refreshActiveSkills()
	activated, deactivated := diffStringSet(prevActive, h.activeSkills)

	if result.Output != "" {
		_, _ = fmt.Fprintln(h.out, result.Output)
	}
	for _, name := range activated {
		_, _ = fmt.Fprintf(h.out, "Skill %q activated.\n", name)
	}
	for _, name := range deactivated {
		_, _ = fmt.Fprintf(h.out, "Skill %q deactivated.\n", name)
	}
	h.emitSessionEvent(session.NewCommandResultEvent(line, result.Output, activated, deactivated))
	logPlainSlashCommand(line, result.Output, activated, deactivated)

	switch result.Action {
	case commands.ActionQuit:
		return true, nil
	case commands.ActionOpenConfig:
		_, _ = fmt.Fprintln(h.out, "Config overlay is not available in plain interactive mode.")
	case commands.ActionOpenWiki:
		_, _ = fmt.Fprintln(h.out, "Wiki overlay is not available in plain interactive mode.")
	}

	return false, nil
}

func (h *plainInteractiveHost) runTurn(ctx context.Context, text string) error {
	if h.agent == nil {
		_, _ = fmt.Fprintln(h.out, persona.ErrorMessage("no agent configured"))
		return nil
	}
	turnCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	ch, err := h.agent.Turn(turnCtx, text)
	if err != nil {
		return err
	}

	wroteText := false
	var assistantText strings.Builder
	h.sessionState.ResetForPrompt(text)
	h.emitSessionEvent(session.NewTurnStartedEvent(text, h.modelName))
	h.emitSessionEvent(session.NewCheckpointCreatedEvent(fmt.Sprintf("turn-%d", h.turnCount+1), "turn_started"))
	toolCallArgs := map[string]string{}
	for evt := range ch {
		switch evt.Type {
		case "text_delta":
			if !wroteText {
				_, _ = fmt.Fprintln(h.out)
				wroteText = true
			}
			assistantText.WriteString(evt.Text)
			_, _ = fmt.Fprint(h.out, evt.Text)
		case "tool_call":
			if evt.ToolCall != nil {
				h.sessionState.ApplyEvent(evt)
				h.emitSessionEvent(session.NewToolCallEvent(evt.ToolCall.ID, evt.ToolCall.Name, evt.ToolCall.Input))
				toolCallArgs[evt.ToolCall.ID] = strings.TrimSpace(string(evt.ToolCall.Input))
				_, _ = fmt.Fprintf(h.out, "\n[tool:%s] %s\n", evt.ToolCall.Name, strings.TrimSpace(string(evt.ToolCall.Input)))
			}
		case "tool_result":
			if evt.ToolResult != nil {
				h.sessionState.ApplyEvent(evt)
				content := evt.ToolResult.DisplayContent
				if content == "" {
					content = evt.ToolResult.Content
				}
				h.emitSessionEvent(session.NewToolResultEvent(evt.ToolResult.ID, evt.ToolResult.Name, content, evt.ToolResult.IsError))
				_, _ = fmt.Fprintf(h.out, "[tool-result:%s] %s\n", evt.ToolResult.Name, strings.TrimSpace(content))
				delete(toolCallArgs, evt.ToolResult.ID)
			}
		case "tool_progress":
			if evt.ToolProgress != nil {
				_, _ = fmt.Fprintf(h.out, "[tool-progress:%s] %s\n", evt.ToolProgress.Name, strings.TrimSpace(evt.ToolProgress.Content))
			}
		case "subagent_done":
			if evt.SubagentResult != nil {
				h.emitSessionEvent(session.NewSubagentDoneEvent(evt.SubagentResult.Name, evt.Text, evt.SubagentResult.Output))
				_, _ = fmt.Fprintf(h.out, "\n[background-task:%s] %s\n", evt.SubagentResult.Name, evt.Text)
			}
		case "error":
			if evt.Error != nil {
				_, _ = fmt.Fprintln(h.out, "\n"+persona.ErrorMessage(evt.Error.Error()))
			}
		case "done":
			if wroteText {
				_, _ = fmt.Fprintln(h.out)
			}
			if strings.TrimSpace(assistantText.String()) != "" {
				h.emitSessionEvent(session.NewAssistantFinalEvent(assistantText.String()))
			}
			h.turnCount++
			h.totalCost += estimatePlainInteractiveCost(h.modelName, evt.InputTokens, evt.OutputTokens)
			if strings.TrimSpace(evt.DiffSummary) != "" {
				_, _ = fmt.Fprintln(h.out, evt.DiffSummary)
			}
			if snapshot := h.DebugVerificationSnapshot(); snapshot != "" {
				gate := session.ParseVerificationGate(snapshot)
				verdict, reason := session.ParseVerificationSnapshot(snapshot)
				if plan := h.sessionState.Plan(); len(plan) > 0 {
					h.emitSessionEvent(session.NewPlanUpdatedEvent("turn_done", plan))
				}
				if gate == "hard_fail" || (gate == "" && verdict != "" && verdict != "passed") {
					h.emitSessionEvent(session.NewGateFailedEvent("verification", reason))
				}
				_, _ = fmt.Fprintln(h.out, snapshot)
				h.emitSessionEvent(session.NewVerificationSnapshotEvent(snapshot))
				log.Printf("verification snapshot: %s", strings.ReplaceAll(strings.TrimSpace(snapshot), "\n", " | "))
			}
			_, _ = fmt.Fprintln(h.out, persona.SuccessMessage())
			_, _ = fmt.Fprintln(h.out, h.statusLine())
			h.emitSessionEvent(session.NewTurnCompletedEvent(evt.DiffSummary, evt.InputTokens, evt.OutputTokens))
		}
	}
	return nil
}

func (h *plainInteractiveHost) DebugVerificationSnapshot() string {
	if h.sessionState == nil {
		return ""
	}
	return h.sessionState.BuildVerificationSnapshot()
}

func (h *plainInteractiveHost) emitSessionEvent(evt session.Event) {
	if h.eventSink != nil {
		h.eventSink.Emit(evt.WithActor(session.PrimaryActor()))
	}
}

func (h *plainInteractiveHost) SetEventSink(sink session.EventSink) {
	h.eventSink = sink
}

func (h *plainInteractiveHost) printSessionHeader() {
	_, _ = fmt.Fprintln(h.out, "Plain interactive mode")
	_, _ = fmt.Fprintln(h.out, h.statusLine())
	if len(h.activeSkills) > 0 {
		_, _ = fmt.Fprintf(h.out, "Active skills: %s\n", summarizePlainActiveSkills(h.activeSkills))
	}
	_, _ = fmt.Fprintln(h.out, "Type /help for commands.")
}

func (h *plainInteractiveHost) statusLine() string {
	parts := []string{
		persona.StatusPrefix(),
		h.modelName,
		fmt.Sprintf("Turn %d/%d", h.turnCount, h.maxTurns),
		fmt.Sprintf("~$%.2f", h.totalCost),
	}
	if h.gitBranch != "" {
		parts = append(parts, "⎇ "+h.gitBranch)
	}
	if len(h.activeSkills) > 0 {
		parts = append(parts, "Skills: "+summarizePlainActiveSkills(h.activeSkills))
	}
	return strings.Join(parts, " | ")
}

func (h *plainInteractiveHost) readLine() (string, error) {
	line, err := h.reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	if err == io.EOF && line == "" {
		return "", io.EOF
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func (h *plainInteractiveHost) readLineCtx(ctx context.Context) (string, error) {
	if ctx == nil {
		return h.readLine()
	}
	type readResult struct {
		line string
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		line, err := h.readLine()
		ch <- readResult{line: line, err: err}
	}()
	select {
	case <-ctx.Done():
		return "", context.Cause(ctx)
	case res := <-ch:
		return res.line, res.err
	}
}

func isDestructiveToolCall(tool string, input json.RawMessage) bool {
	tool = strings.ToLower(strings.TrimSpace(tool))
	switch tool {
	case "shell", "process":
		return true
	case "file":
		var payload map[string]any
		if err := json.Unmarshal(input, &payload); err != nil {
			return false
		}
		op := strings.ToLower(strings.TrimSpace(fmt.Sprint(payload["operation"])))
		return op == "write" || op == "patch" || op == "delete" || op == "move" || op == "rename" || op == "create"
	default:
		return false
	}
}

func diffStringSet(before, after []string) (activated []string, deactivated []string) {
	beforeSet := make(map[string]struct{}, len(before))
	afterSet := make(map[string]struct{}, len(after))
	for _, name := range before {
		beforeSet[name] = struct{}{}
	}
	for _, name := range after {
		afterSet[name] = struct{}{}
		if _, ok := beforeSet[name]; !ok {
			activated = append(activated, name)
		}
	}
	for _, name := range before {
		if _, ok := afterSet[name]; !ok {
			deactivated = append(deactivated, name)
		}
	}
	return activated, deactivated
}

func summarizePlainActiveSkills(active []string) string {
	if len(active) == 0 {
		return ""
	}
	if len(active) <= 2 {
		return strings.Join(active, ", ")
	}
	return fmt.Sprintf("%d active (%s, %s, +%d)", len(active), active[0], active[1], len(active)-2)
}

func estimatePlainInteractiveCost(model string, inputTokens, outputTokens int) float64 {
	switch model {
	case "claude-sonnet-4-5":
		return (float64(inputTokens)/1_000_000)*3.0 + (float64(outputTokens)/1_000_000)*15.0
	case "claude-opus-4-5":
		return (float64(inputTokens)/1_000_000)*15.0 + (float64(outputTokens)/1_000_000)*75.0
	case "claude-haiku-3-5":
		return (float64(inputTokens)/1_000_000)*0.80 + (float64(outputTokens)/1_000_000)*4.0
	case "gpt-4o":
		return (float64(inputTokens)/1_000_000)*2.50 + (float64(outputTokens)/1_000_000)*10.0
	case "gpt-4o-mini":
		return (float64(inputTokens)/1_000_000)*0.15 + (float64(outputTokens)/1_000_000)*0.60
	default:
		return 0.0
	}
}

func logPlainSlashCommand(line, output string, activated, deactivated []string) {
	parts := []string{fmt.Sprintf("slash command: %s", strings.TrimSpace(line))}
	if trimmed := strings.TrimSpace(output); trimmed != "" {
		parts = append(parts, fmt.Sprintf("output=%q", singleLinePlainPreview(trimmed, 160)))
	}
	if len(activated) > 0 {
		parts = append(parts, fmt.Sprintf("activated=%s", strings.Join(activated, ",")))
	}
	if len(deactivated) > 0 {
		parts = append(parts, fmt.Sprintf("deactivated=%s", strings.Join(deactivated, ",")))
	}
	log.Print(strings.Join(parts, " "))
}

func singleLinePlainPreview(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if max > 0 && len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}
