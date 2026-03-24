package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/internal/persona"
	"github.com/julianshen/rubichan/internal/session"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// taskToolName is the tool name used by the subagent/task system.
// This couples to internal/tools.NewTaskTool's Name() return value.
const taskToolName = "task"

// TurnEventMsg wraps an agent.TurnEvent as a Bubble Tea message so streaming
// events from the agent can be dispatched through the Update loop.
type TurnEventMsg agent.TurnEvent

// turnStartedMsg carries the event channel and first event back to Update
// so that m.eventCh is set in the Update goroutine rather than the Cmd goroutine.
type turnStartedMsg struct {
	ch     <-chan agent.TurnEvent
	first  agent.TurnEvent
	cancel context.CancelFunc
}

// Init implements tea.Model. It initializes the input area and starts
// listening for approval requests from the agent.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.input.Init(), m.waitForApproval())
}

// Update implements tea.Model. It processes incoming messages and returns the
// updated model and any commands to execute.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Ctrl+C pre-overlay intercept: must always quit and unblock agent if needed.
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.Type == tea.KeyCtrlC && m.activeOverlay != nil {
		m.quitting = true
		if m.pendingApproval != nil {
			m.pendingApproval.responseValue <- ApprovalNo
			m.pendingApproval = nil
		}
		if m.wikiCancel != nil {
			m.wikiCancel()
		}
		m.activeOverlay = nil
		return m, tea.Quit
	}

	// Generic overlay delegation: route all messages to the active overlay.
	if m.activeOverlay != nil {
		updated, cmd := m.activeOverlay.Update(msg)
		m.activeOverlay = updated
		if m.activeOverlay.Done() {
			result := m.activeOverlay.Result()
			m.activeOverlay = nil
			followUp := m.processOverlayResult(result)
			return m, tea.Batch(cmd, followUp)
		}
		return m, cmd
	}

	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case tea.MouseMsg:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.refreshRenderers()
		m.reflowViewport()
		if m.completion != nil {
			m.completion.SetWidth(m.width)
		}
		if m.fileCompletion != nil {
			m.fileCompletion.SetWidth(m.width)
		}
		return m, nil

	case approvalRequestMsg:
		m.state = StateAwaitingApproval
		workDir := ""
		if m.agent != nil {
			workDir = m.agent.WorkingDir()
		}
		m.activeOverlay = NewApprovalOverlay(msg.tool, msg.input, workDir, m.width, msg.options)
		m.pendingApproval = &approvalRequest{
			tool:          msg.tool,
			input:         msg.input,
			options:       msg.options,
			responseValue: msg.responseValue,
		}
		return m, m.waitForApproval()

	case turnStartedMsg:
		m.eventCh = msg.ch
		m.turnCancel = msg.cancel
		return m.handleTurnEvent(TurnEventMsg(msg.first))

	case TurnEventMsg:
		return m.handleTurnEvent(msg)

	case wikiDoneMsg:
		m.wikiRunning = false
		m.wikiCancel = nil
		m.statusBar.ClearWikiProgress()
		if errors.Is(msg.Err, context.Canceled) || errors.Is(msg.Err, context.DeadlineExceeded) {
			m.content.WriteString("Wiki generation cancelled.\n")
		} else if msg.Err != nil {
			m.content.WriteString(persona.ErrorMessage(fmt.Sprintf("Wiki generation failed: %s", msg.Err)))
		} else {
			m.content.WriteString("Wiki generation complete!\n")
		}
		m.setContentAndAutoScroll()
		return m, nil

	case spinner.TickMsg:
		if m.state == StateStreaming {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}

	return m, nil
}

// handleKeyMsg processes keyboard input.
func (m *Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Ctrl+C always quits, regardless of state.
	if msg.Type == tea.KeyCtrlC {
		m.quitting = true
		// Unblock the agent goroutine if it's waiting for approval.
		if m.pendingApproval != nil {
			m.pendingApproval.responseValue <- ApprovalNo
			m.pendingApproval = nil
		}
		// Cancel any running wiki generation goroutine.
		if m.wikiCancel != nil {
			m.wikiCancel()
		}
		return m, tea.Quit
	}

	// Completion overlay intercepts Tab/Up/Down/Escape when visible,
	// before scroll keys can claim Up/Down.
	if m.state == StateInput && m.completion != nil && m.completion.Visible() {
		switch msg.Type {
		case tea.KeyTab:
			if accepted, value := m.completion.HandleTab(); accepted {
				m.input.SetValue("/" + value + " ")
				m.syncCompletion()
			}
			return m, nil
		case tea.KeyUp, tea.KeyDown:
			m.completion.HandleKey(msg)
			return m, nil
		case tea.KeyEsc:
			m.completion.HandleKey(msg)
			return m, nil
		}
	}

	// File completion overlay for @ mentions.
	if m.state == StateInput && m.fileCompletion != nil && m.fileCompletion.Visible() {
		switch msg.Type {
		case tea.KeyTab:
			if accepted, value := m.fileCompletion.HandleTab(); accepted {
				// Replace the @query with the full path
				cur := m.input.Value()
				atIdx := strings.LastIndex(cur, "@")
				if atIdx >= 0 {
					m.input.SetValue(cur[:atIdx] + "@" + value + " ")
				}
				m.syncCompletion()
			}
			return m, nil
		case tea.KeyUp, tea.KeyDown:
			m.fileCompletion.HandleKey(msg)
			return m, nil
		case tea.KeyEsc:
			m.fileCompletion.HandleKey(msg)
			return m, nil
		}
	}

	// Ctrl+P/N for input history navigation.
	if m.state == StateInput && m.history != nil {
		if msg.Type == tea.KeyCtrlP {
			if val, ok := m.history.Previous(m.input.Value()); ok {
				m.input.SetValue(val)
				m.syncCompletion()
			}
			return m, nil
		}
		if msg.Type == tea.KeyCtrlN {
			if val, ok := m.history.Next(); ok {
				m.input.SetValue(val)
				m.syncCompletion()
			}
			return m, nil
		}
	}

	// Scroll keys are forwarded to the viewport regardless of state.
	if isScrollKey(msg) {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	// Ctrl+T toggles collapse/expand on all tool results.
	if msg.Type == tea.KeyCtrlT && m.state == StateInput && m.content.ToolResultCount() > 0 {
		m.content.ToggleAllToolResults()
		m.viewport.SetContent(m.viewportContent())
		return m, nil
	}

	// Ctrl+E toggles full expansion on the most recent truncated tool result.
	if msg.Type == tea.KeyCtrlE && m.state == StateInput && m.content.ToolResultCount() > 0 {
		m.content.ToggleFullExpandMostRecent()
		m.viewport.SetContent(m.viewportContent())
		return m, nil
	}

	if msg.Type == tea.KeyCtrlG && m.state == StateInput && strings.TrimSpace(m.diffSummary) != "" {
		m.diffExpanded = !m.diffExpanded
		m.viewport.SetContent(m.viewportContent())
		return m, nil
	}

	switch msg.Type {
	case tea.KeyEnter:
		if m.state != StateInput {
			return m, nil
		}
		text := strings.TrimSpace(m.input.Value())
		if text == "" {
			return m, nil
		}
		if m.history != nil {
			m.history.Add(text)
		}
		m.input.Reset()
		m.syncCompletion()

		if directive, ok, err := commands.RewriteInlineSkillDirective(text); ok {
			if err != nil {
				m.content.WriteString(persona.ErrorMessage(err.Error()))
				m.setContentAndAutoScroll()
				return m, nil
			}
			m.content.WriteString(fmt.Sprintf("Inline skill directive: %s %q\n", directive.Action, directive.Name))
			m.setContentAndAutoScroll()
			cmd := m.handleCommandParts(directive.Command, directive.Args)
			return m, cmd
		}

		if strings.HasPrefix(text, "/") {
			cmd := m.handleCommand(text)
			return m, cmd
		}

		// Regular user message: write to content and start agent turn.
		// Reset per-turn state.
		m.diffSummary = ""
		m.diffExpanded = false
		m.toolCallArgs = nil
		m.content.WriteString(styleUserPrompt.Render("❯ ") + text + "\n")
		m.viewport.SetContent(m.viewportContent())
		m.viewport.GotoBottom()
		m.lastPrompt = text
		if m.sessionState != nil {
			m.sessionState.ResetForPrompt(text)
		}
		m.emitSessionEvent(session.NewTurnStartedEvent(text, m.modelName))
		m.emitSessionEvent(session.NewCheckpointCreatedEvent(fmt.Sprintf("turn-%d", m.turnCount+1), "turn_started"))
		m.assistantStartIdx = m.content.LenWithWidth(m.width)
		m.assistantEndIdx = m.assistantStartIdx
		m.state = StateStreaming
		m.thinkingMsg = persona.ThinkingMessage()
		m.statusBar.ClearElapsed()
		m.turnStartTime = time.Now()

		return m, tea.Batch(m.startTurn(m.agent, text), m.spinner.Tick)

	default:
		// Forward key to input area
		if m.state == StateInput {
			prevHeight := m.input.Height()
			cmd := m.input.Update(msg)
			if m.input.Height() != prevHeight {
				m.reflowViewport()
			}
			m.syncCompletion()
			return m, cmd
		}
		return m, nil
	}
}

// startTurn initiates an agent turn and returns a tea.Cmd that sends back a
// turnStartedMsg carrying the channel and first event. The agent is captured
// as a parameter (not read from m.agent in the closure) to avoid a data race
// between the Cmd goroutine and the Update goroutine.
func (m *Model) startTurn(a *agent.Agent, text string) tea.Cmd {
	return func() tea.Msg {
		if a == nil {
			return TurnEventMsg(agent.TurnEvent{
				Type:  "error",
				Error: fmt.Errorf("no agent configured"),
			})
		}

		turnCtx, cancel := context.WithCancel(context.Background())
		ch, err := a.Turn(turnCtx, text)
		if err != nil {
			cancel()
			return TurnEventMsg(agent.TurnEvent{
				Type:  "error",
				Error: fmt.Errorf("turn failed: %w", err),
			})
		}

		// Read first event in the Cmd goroutine, but pass the channel
		// back via turnStartedMsg so Update sets m.eventCh safely.
		evt, ok := <-ch
		if !ok {
			cancel()
			return TurnEventMsg(agent.TurnEvent{Type: "done"})
		}
		return turnStartedMsg{
			ch:     ch,
			first:  evt,
			cancel: cancel,
		}
	}
}

// waitForEvent returns a tea.Cmd that reads the next event from the event
// channel and returns it as a TurnEventMsg.
func (m *Model) waitForEvent() tea.Cmd {
	ch := m.eventCh
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		evt, ok := <-ch
		if !ok {
			return TurnEventMsg(agent.TurnEvent{Type: "done"})
		}
		return TurnEventMsg(evt)
	}
}

// isScrollKey returns true if the key message is a viewport scroll key.
func isScrollKey(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyPgUp, tea.KeyPgDown, tea.KeyHome, tea.KeyEnd:
		return true
	case tea.KeyUp, tea.KeyDown:
		return true
	}
	// Ctrl+U / Ctrl+D for half-page scroll.
	if msg.Type == tea.KeyCtrlU || msg.Type == tea.KeyCtrlD {
		return true
	}
	return false
}

// setContentAndAutoScroll updates the viewport content and scrolls to bottom only
// if the viewport was already at the bottom before the update. This preserves the
// user's scroll position when they scroll up to read earlier content.
func (m *Model) setContentAndAutoScroll() {
	wasAtBottom := m.viewport.AtBottom()
	m.viewport.SetContent(m.viewportContent())
	if wasAtBottom {
		m.viewport.GotoBottom()
	}
}

// handleTurnEvent processes a streaming TurnEvent from the agent.
func (m *Model) handleTurnEvent(msg TurnEventMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case "text_delta":
		if m.rawAssistant.Len() == 0 {
			m.assistantStartIdx = m.content.LenWithWidth(m.width)
			m.assistantEndIdx = m.assistantStartIdx
		}
		m.rawAssistant.WriteString(msg.Text)
		m.replaceAssistantContent(SanitizeAssistantOutput(m.rawAssistant.String()))
		if IsMarkdownBreakpoint(m.rawAssistant.String()) {
			m.renderAssistantMarkdown()
		}
		m.setContentAndAutoScroll()
		return m, m.waitForEvent()

	case "tool_call":
		name := ""
		args := ""
		if msg.ToolCall != nil {
			name = msg.ToolCall.Name
			args = string(msg.ToolCall.Input)
			if m.sessionState != nil {
				m.sessionState.ApplyEvent(agentsdk.TurnEvent(msg))
			}
			m.emitSessionEvent(session.NewToolCallEvent(msg.ToolCall.ID, msg.ToolCall.Name, msg.ToolCall.Input))
			// Cache args by tool use ID so tool_result can look them up.
			if m.toolCallArgs == nil {
				m.toolCallArgs = make(map[string]string)
			}
			m.toolCallArgs[msg.ToolCall.ID] = args

			// Track subagent launches in the status bar.
			// Coupled to the "task" tool name from internal/tools.NewTaskTool.
			if strings.EqualFold(name, taskToolName) {
				var taskArgs struct {
					Description string `json:"description"`
				}
				if err := json.Unmarshal([]byte(args), &taskArgs); err != nil {
					if m.debug {
						log.Printf("[debug] failed to parse task tool args: %v", err)
					}
					m.statusBar.SetSubagent("subagent")
				} else if taskArgs.Description != "" {
					m.statusBar.SetSubagent(taskArgs.Description)
				} else {
					m.statusBar.SetSubagent("subagent")
				}
			}
		}
		// Rotate thinking message on each tool call (state update, not render).
		m.thinkingMsg = persona.ThinkingMessage()
		m.content.WriteString(m.toolBox.RenderToolCall(name, args))
		m.setContentAndAutoScroll()
		return m, m.waitForEvent()

	case "tool_result":
		resultContent := ""
		resultName := ""
		isError := false
		if msg.ToolResult != nil {
			if m.sessionState != nil {
				m.sessionState.ApplyEvent(agentsdk.TurnEvent(msg))
			}
			// Prefer DisplayContent for user-facing output; fall back to Content.
			resultContent = msg.ToolResult.DisplayContent
			if resultContent == "" {
				resultContent = msg.ToolResult.Content
			}
			resultName = msg.ToolResult.Name
			isError = msg.ToolResult.IsError
			m.emitSessionEvent(session.NewToolResultEvent(msg.ToolResult.ID, msg.ToolResult.Name, resultContent, msg.ToolResult.IsError))
		}
		lineCount := strings.Count(resultContent, "\n") + 1
		if resultContent == "" {
			lineCount = 0
		}
		args := ""
		if msg.ToolResult != nil {
			args = m.toolCallArgs[msg.ToolResult.ID]
		}
		cr := CollapsibleToolResult{
			Name:      resultName,
			Args:      args,
			Content:   resultContent,
			LineCount: lineCount,
			IsError:   isError,
			Collapsed: false, // expanded during streaming
			ToolType:  ClassifyTool(resultName),
		}
		m.content.AppendToolResult(cr)
		m.setContentAndAutoScroll()
		return m, m.waitForEvent()

	case "tool_progress":
		if msg.ToolProgress != nil {
			m.content.WriteString(m.toolBox.RenderToolProgress(
				msg.ToolProgress.Name,
				msg.ToolProgress.Stage.String(),
				msg.ToolProgress.Content,
				msg.ToolProgress.IsError,
			))
			m.setContentAndAutoScroll()
		}
		return m, m.waitForEvent()

	case "ui_request":
		if msg.UIRequest != nil && m.debug {
			m.content.WriteString(fmt.Sprintf("[ui_request] kind=%s id=%s\n", msg.UIRequest.Kind, msg.UIRequest.ID))
			m.setContentAndAutoScroll()
		}
		return m, m.waitForEvent()

	case "ui_response":
		if msg.UIResponse != nil && m.debug {
			m.content.WriteString(fmt.Sprintf("[ui_response] id=%s action=%s\n", msg.UIResponse.RequestID, msg.UIResponse.ActionID))
			m.setContentAndAutoScroll()
		}
		return m, m.waitForEvent()

	case "ui_update":
		// TODO: consume ui_update payloads for long-running interactive flows
		// once the runtime starts emitting incremental UI progress updates.
		if msg.UIUpdate != nil && m.debug {
			m.content.WriteString(fmt.Sprintf("[ui_update] id=%s status=%s\n", msg.UIUpdate.RequestID, msg.UIUpdate.Status))
			m.setContentAndAutoScroll()
		}
		return m, m.waitForEvent()

	case "subagent_done":
		m.statusBar.SetSubagent("")
		summary := msg.Text
		output := ""
		if summary == "" && msg.SubagentResult != nil {
			summary = fmt.Sprintf("[Background task completed (agent: %s)]", msg.SubagentResult.Name)
		}
		if msg.SubagentResult != nil {
			output = msg.SubagentResult.Output
			m.emitSessionEvent(session.NewSubagentDoneEvent(msg.SubagentResult.Name, summary, output))
		}
		m.content.WriteString(fmt.Sprintf("\n--- Background Task ---\n%s\n-----------------------\n", summary))
		m.setContentAndAutoScroll()
		return m, m.waitForEvent()

	case "error":
		errMsg := "unknown error"
		if msg.Error != nil {
			errMsg = msg.Error.Error()
		}
		m.rawAssistant.Reset()
		m.content.WriteString(persona.ErrorMessage(errMsg))
		m.setContentAndAutoScroll()
		return m, m.waitForEvent()

	case "done":
		if m.turnCancel != nil {
			m.turnCancel()
			m.turnCancel = nil
		}
		if !m.turnStartTime.IsZero() {
			m.statusBar.SetElapsed(time.Since(m.turnStartTime))
			m.turnStartTime = time.Time{}
		}
		raw := m.rawAssistant.String()
		visible := SanitizeAssistantOutput(raw)
		m.renderAssistantMarkdown()
		m.rawAssistant.Reset()
		if strings.TrimSpace(visible) != "" {
			m.emitSessionEvent(session.NewAssistantFinalEvent(visible))
		}
		m.diffSummary = msg.DiffSummary
		m.diffExpanded = false
		// Collapse all tool results from this turn.
		m.content.CollapseAllToolResults()
		if summary := m.DebugVerificationSnapshot(); summary != "" {
			gate := session.ParseVerificationGate(summary)
			verdict, reason := session.ParseVerificationSnapshot(summary)
			if m.sessionState != nil {
				if plan := m.sessionState.Plan(); len(plan) > 0 {
					m.emitSessionEvent(session.NewPlanUpdatedEvent("turn_done", plan))
				}
			}
			if gate == "hard_fail" || (gate == "" && verdict != "" && verdict != "passed") {
				m.emitSessionEvent(session.NewGateFailedEvent("verification", reason))
			}
			if m.debug {
				m.content.WriteString(summary)
				if !strings.HasSuffix(summary, "\n") {
					m.content.WriteString("\n")
				}
			}
			m.emitSessionEvent(session.NewVerificationSnapshotEvent(summary))
			if m.debug {
				log.Printf("verification snapshot: %s", strings.ReplaceAll(strings.TrimSpace(summary), "\n", " | "))
			}
		}
		m.content.WriteString(persona.SuccessMessage())
		m.content.WriteString("\n")
		m.setContentAndAutoScroll()

		// Update status bar with token usage and turn count.
		m.turnCount++
		contextBudget := 100000
		if m.cfg != nil && m.cfg.Agent.ContextBudget > 0 {
			contextBudget = m.cfg.Agent.ContextBudget
		}
		m.statusBar.SetTokens(msg.InputTokens, contextBudget)
		m.statusBar.SetTurn(m.turnCount, m.maxTurns)
		cost := EstimateCost(m.modelName, msg.InputTokens, msg.OutputTokens)
		m.totalCost += cost
		m.statusBar.SetCost(m.totalCost)
		m.emitSessionEvent(session.NewTurnCompletedEvent(msg.DiffSummary, msg.InputTokens, msg.OutputTokens))

		m.state = StateInput
		m.eventCh = nil
		if m.ralph != nil {
			if cmd := m.advanceRalphLoop(visible); cmd != nil {
				return m, tea.Batch(cmd, m.spinner.Tick)
			}
		}
		return m, nil
	}

	return m, m.waitForEvent()
}

// renderAssistantMarkdown re-renders the accumulated rawAssistant markdown
// through the Glamour renderer and replaces content from assistantStartIdx.
// If rendering fails or produces empty output, the existing raw content is
// kept unchanged.
func (m *Model) renderAssistantMarkdown() {
	raw := SanitizeAssistantOutput(m.rawAssistant.String())
	if raw == "" {
		return
	}
	rendered, err := m.mdRenderer.Render(raw)
	if err != nil || rendered == "" {
		return
	}
	m.replaceAssistantContent(rendered)
}

// replaceAssistantContent swaps only the assistant's display slice, preserving
// any tool output appended after the assistant started streaming.
func (m *Model) replaceAssistantContent(text string) {
	contentStr := m.content.Render(m.width)
	if m.assistantStartIdx > len(contentStr) {
		return
	}
	if m.assistantEndIdx < m.assistantStartIdx {
		m.assistantEndIdx = len(contentStr)
	}
	if m.assistantEndIdx > len(contentStr) {
		m.assistantEndIdx = len(contentStr)
	}

	m.content.ReplaceTextRangeWithWidth(m.width, m.assistantStartIdx, m.assistantEndIdx, text)
	m.assistantEndIdx = m.assistantStartIdx + len(text)
}

func (m *Model) advanceRalphLoop(raw string) tea.Cmd {
	if m.ralph == nil {
		return nil
	}

	loop := m.ralph
	switch {
	case loop.cancelled:
		m.content.WriteString("Ralph loop stopped.\n")
		m.setContentAndAutoScroll()
		m.ralph = nil
		return nil
	case strings.Contains(raw, loop.cfg.CompletionPromise):
		m.content.WriteString(fmt.Sprintf("Ralph loop complete after %d iteration(s).\n", loop.iteration+1))
		m.setContentAndAutoScroll()
		m.ralph = nil
		return nil
	case loop.iteration+1 >= loop.cfg.MaxIterations:
		m.content.WriteString(fmt.Sprintf("Ralph loop stopped after reaching %d iteration(s) without completion promise %q.\n", loop.cfg.MaxIterations, loop.cfg.CompletionPromise))
		m.setContentAndAutoScroll()
		m.ralph = nil
		return nil
	}

	loop.iteration++
	prompt := loop.cfg.Prompt
	m.diffSummary = ""
	m.diffExpanded = false
	m.content.WriteString(styleUserPrompt.Render("❯ ") + prompt + "\n")
	m.setContentAndAutoScroll()
	m.assistantStartIdx = m.content.LenWithWidth(m.width)
	m.assistantEndIdx = m.assistantStartIdx
	m.state = StateStreaming
	m.thinkingMsg = persona.ThinkingMessage()
	m.turnStartTime = time.Now()
	return m.startTurn(m.agent, prompt)
}
