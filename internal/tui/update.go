package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/persona"
)

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
	// Route messages to config form overlay when active.
	if m.state == StateConfigOverlay && m.configForm != nil {
		form, cmd := m.configForm.Form().Update(msg)
		if f, ok := form.(*huh.Form); ok {
			m.configForm.SetForm(f)
		}
		switch m.configForm.Form().State {
		case huh.StateCompleted:
			_ = m.configForm.Save()
			m.state = StateInput
			m.configForm = nil
		case huh.StateAborted:
			m.state = StateInput
			m.configForm = nil
		}
		return m, cmd
	}

	// Route messages to wiki form overlay when active.
	if m.state == StateWikiOverlay && m.wikiForm != nil {
		form, cmd := m.wikiForm.Form().Update(msg)
		if f, ok := form.(*huh.Form); ok {
			m.wikiForm.SetForm(f)
		}
		switch m.wikiForm.Form().State {
		case huh.StateCompleted:
			m.state = StateInput
			wf := m.wikiForm
			m.wikiForm = nil
			return m, m.startWikiGeneration(wf)
		case huh.StateAborted:
			m.state = StateInput
			m.wikiForm = nil
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
		// Reserve space for header (1), divider (1), status (1), input (1)
		viewportHeight := m.height - 4
		if viewportHeight < 1 {
			viewportHeight = 1
		}
		m.viewport.Width = m.width
		m.viewport.Height = viewportHeight
		if m.completion != nil {
			m.completion.SetWidth(m.width)
		}
		if m.fileCompletion != nil {
			m.fileCompletion.SetWidth(m.width)
		}
		return m, nil

	case approvalRequestMsg:
		m.state = StateAwaitingApproval
		m.approvalPrompt = NewApprovalPrompt(msg.tool, msg.input, m.width, nil)
		m.pendingApproval = &approvalRequest{
			tool:     msg.tool,
			input:    msg.input,
			response: msg.response,
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
			m.pendingApproval.response <- false
			m.pendingApproval = nil
		}
		// Cancel any running wiki generation goroutine.
		if m.wikiCancel != nil {
			m.wikiCancel()
		}
		return m, tea.Quit
	}

	// Delegate to approval prompt when awaiting approval.
	if m.state == StateAwaitingApproval && m.approvalPrompt != nil {
		if m.approvalPrompt.HandleKey(msg) {
			result := m.approvalPrompt.Result()
			approved := result == ApprovalYes || result == ApprovalAlways
			if result == ApprovalAlways {
				m.alwaysDenied.Delete(m.pendingApproval.tool)
				m.alwaysApproved.Store(m.pendingApproval.tool, true)
			}
			if result == ApprovalDenyAlways {
				m.alwaysApproved.Delete(m.pendingApproval.tool)
				m.alwaysDenied.Store(m.pendingApproval.tool, true)
			}
			m.pendingApproval.response <- approved
			m.approvalPrompt = nil
			m.pendingApproval = nil
			m.state = StateStreaming
			return m, m.waitForEvent()
		}
		return m, nil
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
	if msg.Type == tea.KeyCtrlT && m.state == StateInput && len(m.toolResults) > 0 {
		// If any are collapsed, expand all; otherwise collapse all.
		anyCollapsed := false
		for _, tr := range m.toolResults {
			if tr.Collapsed {
				anyCollapsed = true
				break
			}
		}
		for i := range m.toolResults {
			m.toolResults[i].Collapsed = !anyCollapsed
		}
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

		if strings.HasPrefix(text, "/") {
			cmd := m.handleCommand(text)
			return m, cmd
		}

		// Regular user message: write to content and start agent turn.
		// Reset per-turn state.
		m.diffSummary = ""
		m.diffExpanded = false
		m.toolResults = nil
		m.nextToolResultID = 0
		m.toolCallArgs = nil
		m.content.WriteString(styleUserPrompt.Render("❯ ") + text + "\n")
		m.viewport.SetContent(m.viewportContent())
		m.viewport.GotoBottom()
		m.assistantStartIdx = m.content.Len()
		m.assistantEndIdx = m.assistantStartIdx
		m.state = StateStreaming
		m.statusBar.ClearElapsed()
		m.turnStartTime = time.Now()

		return m, tea.Batch(m.startTurn(m.agent, text), m.spinner.Tick)

	default:
		// Forward key to input area
		if m.state == StateInput {
			cmd := m.input.Update(msg)
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
			m.assistantStartIdx = m.content.Len()
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
			// Cache args by tool use ID so tool_result can look them up.
			if m.toolCallArgs == nil {
				m.toolCallArgs = make(map[string]string)
			}
			m.toolCallArgs[msg.ToolCall.ID] = args
		}
		m.content.WriteString(m.toolBox.RenderToolCall(name, args))
		m.setContentAndAutoScroll()
		return m, m.waitForEvent()

	case "tool_result":
		resultContent := ""
		resultName := ""
		isError := false
		if msg.ToolResult != nil {
			// Prefer DisplayContent for user-facing output; fall back to Content.
			resultContent = msg.ToolResult.DisplayContent
			if resultContent == "" {
				resultContent = msg.ToolResult.Content
			}
			resultName = msg.ToolResult.Name
			isError = msg.ToolResult.IsError
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
			ID:        m.nextToolResultID,
			Name:      resultName,
			Args:      args,
			Content:   resultContent,
			LineCount: lineCount,
			IsError:   isError,
			Collapsed: false, // expanded during streaming
		}
		m.toolResults = append(m.toolResults, cr)
		m.content.WriteString(toolResultPlaceholder(m.nextToolResultID))
		m.nextToolResultID++
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

	case "subagent_done":
		summary := msg.Text
		if summary == "" && msg.SubagentResult != nil {
			summary = fmt.Sprintf("[Background task completed (agent: %s)]", msg.SubagentResult.Name)
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
		m.diffSummary = msg.DiffSummary
		m.diffExpanded = false
		// Collapse all tool results from this turn.
		for i := range m.toolResults {
			m.toolResults[i].Collapsed = true
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
		m.replaceAssistantContent("")
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
	contentStr := m.content.String()
	if m.assistantStartIdx > len(contentStr) {
		return
	}
	if m.assistantEndIdx < m.assistantStartIdx {
		m.assistantEndIdx = len(contentStr)
	}
	if m.assistantEndIdx > len(contentStr) {
		m.assistantEndIdx = len(contentStr)
	}

	m.content.Reset()
	m.content.WriteString(contentStr[:m.assistantStartIdx])
	m.content.WriteString(text)
	m.content.WriteString(contentStr[m.assistantEndIdx:])
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
	m.assistantStartIdx = m.content.Len()
	m.assistantEndIdx = m.assistantStartIdx
	m.state = StateStreaming
	m.turnStartTime = time.Now()
	return m.startTurn(m.agent, prompt)
}
