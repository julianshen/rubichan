package agentsdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Option is a functional option for configuring an Agent.
type Option func(*Agent)

// WithTools attaches a tool registry to the agent.
func WithTools(r *Registry) Option {
	return func(a *Agent) { a.tools = r }
}

// WithModel overrides the model in the agent config.
func WithModel(model string) Option {
	return func(a *Agent) { a.config.Model = model }
}

// WithApproval attaches an approval function for interactive tool approval.
func WithApproval(fn ApprovalFunc) Option {
	return func(a *Agent) { a.approve = fn }
}

// WithApprovalChecker attaches an input-sensitive approval checker.
func WithApprovalChecker(checker ApprovalChecker) Option {
	return func(a *Agent) { a.approvalChecker = checker }
}

// WithUIRequestHandler attaches a generalized UI interaction handler.
// This is an extension point for adapters that support rich interactions
// (menus/forms/approval flows) beyond fixed yes/no prompts.
func WithUIRequestHandler(handler UIRequestHandler) Option {
	return func(a *Agent) { a.uiRequestHandler = handler }
}

// WithSystemPrompt sets the system prompt.
func WithSystemPrompt(prompt string) Option {
	return func(a *Agent) { a.config.SystemPrompt = prompt }
}

// WithLogger attaches a structured logger. If not set, DefaultLogger() is used.
func WithLogger(l Logger) Option {
	return func(a *Agent) { a.logger = l }
}

// WithConfig overrides the default agent configuration.
func WithConfig(cfg AgentConfig) Option {
	return func(a *Agent) { a.config = cfg }
}

// Agent orchestrates the conversation loop between the user, LLM, and tools.
type Agent struct {
	provider         LLMProvider
	tools            *Registry
	config           AgentConfig
	conversation     *Conversation
	approve          ApprovalFunc
	approvalChecker  ApprovalChecker
	uiRequestHandler UIRequestHandler
	logger           Logger
	turnMu           sync.Mutex
}

const maxUIRequestInputBytes = 2048

// NewAgent creates a new Agent with the given LLM provider and options.
// Panics if provider is nil.
func NewAgent(provider LLMProvider, opts ...Option) *Agent {
	if provider == nil {
		panic("agentsdk: NewAgent called with nil provider")
	}
	a := &Agent{
		provider: provider,
		config:   DefaultAgentConfig(),
		logger:   DefaultLogger(),
	}
	for _, opt := range opts {
		opt(a)
	}
	if a.tools == nil {
		a.tools = NewRegistry()
	}
	a.conversation = NewConversation(a.config.SystemPrompt)
	return a
}

// ErrEmptyMessage is returned by Turn when the user message is empty.
var ErrEmptyMessage = errors.New("agentsdk: empty user message")
var errUIDenyAlways = errors.New("agentsdk: ui deny-always")

// Turn initiates a new agent turn with the given user message. It returns a
// channel of TurnEvent that streams events as the agent processes the turn.
// Concurrent calls are serialized. The caller must consume all events from
// the returned channel to avoid goroutine leaks.
func (a *Agent) Turn(ctx context.Context, userMessage string) (<-chan TurnEvent, error) {
	if userMessage == "" {
		return nil, ErrEmptyMessage
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	a.turnMu.Lock()

	a.conversation.AddUser(userMessage)

	ch := make(chan TurnEvent, 64)
	go func() {
		defer a.turnMu.Unlock()
		defer close(ch)
		a.runLoop(ctx, ch, 0)
	}()
	return ch, nil
}

// Conversation returns the agent's conversation for external inspection.
// It is only safe to call after the Turn event channel has been fully drained.
func (a *Agent) Conversation() *Conversation {
	return a.conversation
}

// runLoop iteratively processes LLM responses and tool calls.
func (a *Agent) runLoop(ctx context.Context, ch chan<- TurnEvent, turnCount int) {
	var totalInputTokens, totalOutputTokens int

	maxTurns := a.config.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 50
	}

	for ; turnCount < maxTurns; turnCount++ {
		if ctx.Err() != nil {
			ch <- TurnEvent{Type: "error", Error: ctx.Err()}
			ch <- a.makeDoneEvent(totalInputTokens, totalOutputTokens)
			return
		}

		toolDefs := a.tools.All()

		req := CompletionRequest{
			Model:     a.config.Model,
			System:    a.conversation.SystemPrompt(),
			Messages:  a.conversation.Messages(),
			Tools:     toolDefs,
			MaxTokens: a.config.MaxOutputTokens,
		}

		stream, err := a.provider.Stream(ctx, req)
		if err != nil {
			ch <- TurnEvent{Type: "error", Error: fmt.Errorf("provider stream: %w", err)}
			ch <- a.makeDoneEvent(totalInputTokens, totalOutputTokens)
			return
		}

		sr := a.consumeStream(ctx, ch, stream, &totalInputTokens, &totalOutputTokens)
		if sr.cancelled || sr.hadError {
			ch <- a.makeDoneEvent(totalInputTokens, totalOutputTokens)
			return
		}

		if len(sr.blocks) > 0 {
			a.conversation.AddAssistant(sr.blocks)
		}

		if len(sr.pendingTools) == 0 {
			ch <- a.makeDoneEvent(totalInputTokens, totalOutputTokens)
			return
		}

		if cancelled := a.executeTools(ctx, ch, sr.pendingTools); cancelled {
			ch <- TurnEvent{Type: "error", Error: ctx.Err()}
			ch <- a.makeDoneEvent(totalInputTokens, totalOutputTokens)
			return
		}
	}

	ch <- TurnEvent{Type: "error", Error: fmt.Errorf("max turns (%d) exceeded", maxTurns)}
	ch <- a.makeDoneEvent(totalInputTokens, totalOutputTokens)
}

// streamResult holds the output of consumeStream.
type streamResult struct {
	blocks       []ContentBlock
	pendingTools []ToolUseBlock
	cancelled    bool
	hadError     bool
}

// consumeStream reads the provider stream, accumulating content blocks and
// tool calls.
func (a *Agent) consumeStream(
	ctx context.Context,
	ch chan<- TurnEvent,
	stream <-chan StreamEvent,
	totalInput, totalOutput *int,
) streamResult {
	var blocks []ContentBlock
	var pendingTools []ToolUseBlock
	var currentTextBuf string
	var currentTool *ToolUseBlock
	var toolInputBuf string

	finalizeTool := func() {
		if currentTool != nil {
			currentTool.Input = json.RawMessage(toolInputBuf)
			pendingTools = append(pendingTools, *currentTool)
			blocks = append(blocks, ContentBlock{
				Type:  "tool_use",
				ID:    currentTool.ID,
				Name:  currentTool.Name,
				Input: currentTool.Input,
			})
			currentTool = nil
			toolInputBuf = ""
		}
	}

	finalizeText := func() {
		if currentTextBuf != "" {
			blocks = append(blocks, ContentBlock{
				Type: "text",
				Text: currentTextBuf,
			})
			currentTextBuf = ""
		}
	}

	var hadError bool
	for event := range stream {
		*totalInput += event.InputTokens
		*totalOutput += event.OutputTokens

		switch event.Type {
		case "text_delta":
			// During tool accumulation, text deltas carry JSON input
			// for the tool call, not user-visible text.
			if currentTool != nil {
				toolInputBuf += event.Text
			} else {
				currentTextBuf += event.Text
				ch <- TurnEvent{Type: "text_delta", Text: event.Text}
			}
		case "tool_use":
			finalizeText()
			finalizeTool()
			currentTool = &ToolUseBlock{
				ID:   event.ToolUse.ID,
				Name: event.ToolUse.Name,
			}
		case "error":
			a.logger.Error("stream error: %v", event.Error)
			ch <- TurnEvent{Type: "error", Error: event.Error}
			// Discard all accumulated state to avoid processing
			// data from a corrupt/partial stream.
			currentTool = nil
			toolInputBuf = ""
			currentTextBuf = ""
			blocks = nil
			pendingTools = nil
			hadError = true
		case "stop":
			// handled after loop
		}
	}

	if !hadError {
		finalizeText()
		finalizeTool()
	}

	return streamResult{
		blocks:       blocks,
		pendingTools: pendingTools,
		cancelled:    ctx.Err() != nil,
		hadError:     hadError,
	}
}

// executeTools runs the pending tool calls. Returns true if context cancelled.
func (a *Agent) executeTools(ctx context.Context, ch chan<- TurnEvent, pendingTools []ToolUseBlock) bool {
	for _, tc := range pendingTools {
		if ctx.Err() != nil {
			return true
		}

		ch <- TurnEvent{
			Type: "tool_call",
			ToolCall: &ToolCallEvent{
				ID:    tc.ID,
				Name:  tc.Name,
				Input: tc.Input,
			},
		}

		result := a.executeSingleTool(ctx, ch, tc)
		a.conversation.AddToolResult(tc.ID, result.content, result.isError)
		ch <- result.event
	}
	return false
}

// toolResult holds the result of a single tool execution.
type toolResult struct {
	content string
	isError bool
	event   TurnEvent
}

func (a *Agent) executeSingleTool(ctx context.Context, ch chan<- TurnEvent, tc ToolUseBlock) toolResult {
	// Check approval if checker is configured.
	if a.approvalChecker != nil {
		result := a.approvalChecker.CheckApproval(tc.Name, tc.Input)
		if result == AutoDenied {
			return a.toolError(tc, "Tool call denied by user (deny-always).")
		}
		if result == ApprovalRequired {
			if a.uiRequestHandler == nil && a.approve == nil {
				return a.toolError(tc, "approval function not configured")
			}
			approved, err := a.requestToolApproval(ctx, ch, tc)
			if err != nil {
				if errors.Is(err, errUIDenyAlways) {
					return a.toolError(tc, "Tool call denied by user (deny-always).")
				}
				a.logger.Error("approval failure for tool %s: %v", tc.Name, err)
				return a.toolError(tc, "approval error")
			}
			if !approved {
				return a.toolError(tc, "tool call denied by user")
			}
		}
	} else if a.approve != nil || a.uiRequestHandler != nil {
		// No checker — always ask for approval.
		approved, err := a.requestToolApproval(ctx, ch, tc)
		if err != nil {
			if errors.Is(err, errUIDenyAlways) {
				return a.toolError(tc, "Tool call denied by user (deny-always).")
			}
			a.logger.Error("approval failure for tool %s: %v", tc.Name, err)
			return a.toolError(tc, "approval error")
		}
		if !approved {
			return a.toolError(tc, "tool call denied by user")
		}
	}

	// Look up and execute the tool.
	tool, ok := a.tools.Get(tc.Name)
	if !ok {
		return a.toolError(tc, fmt.Sprintf("unknown tool: %s", tc.Name))
	}

	// Use streaming execution if available.
	if st, ok := tool.(StreamingTool); ok {
		emit := func(ev ToolEvent) {
			ch <- TurnEvent{
				Type: "tool_progress",
				ToolProgress: &ToolProgressEvent{
					ID:      tc.ID,
					Name:    tc.Name,
					Stage:   ev.Stage,
					Content: ev.Content,
					IsError: ev.IsError,
				},
			}
		}
		res, err := st.ExecuteStream(ctx, tc.Input, emit)
		if err != nil {
			return a.toolError(tc, fmt.Sprintf("tool error: %v", err))
		}
		return toolResult{
			content: res.Content,
			isError: res.IsError,
			event:   makeResultEvent(tc.ID, tc.Name, res),
		}
	}

	res, err := tool.Execute(ctx, tc.Input)
	if err != nil {
		return a.toolError(tc, fmt.Sprintf("tool error: %v", err))
	}
	return toolResult{
		content: res.Content,
		isError: res.IsError,
		event:   makeResultEvent(tc.ID, tc.Name, res),
	}
}

func (a *Agent) requestToolApproval(ctx context.Context, ch chan<- TurnEvent, tc ToolUseBlock) (bool, error) {
	if a.uiRequestHandler != nil {
		req := UIRequest{
			ID:      tc.ID,
			Kind:    UIKindApproval,
			Title:   fmt.Sprintf("Approve %s tool call", tc.Name),
			Message: "Review and choose how to proceed.",
			Actions: []UIAction{
				{ID: "allow", Label: "Allow", Default: true},
				{ID: "deny", Label: "Deny", Style: "danger"},
				{ID: "allow_always", Label: "Always Allow"},
				{ID: "deny_always", Label: "Always Deny", Style: "danger"},
			},
			Metadata: map[string]string{
				"tool":  tc.Name,
				"input": truncateUIInput(tc.Input),
			},
		}
		ch <- TurnEvent{Type: "ui_request", UIRequest: &req}
		resp, err := a.uiRequestHandler.Request(ctx, req)
		if err != nil {
			return false, err
		}
		ch <- TurnEvent{Type: "ui_response", UIResponse: &resp}

		switch strings.ToLower(resp.ActionID) {
		case "allow", "allow_always", "yes":
			return true, nil
		case "deny_always":
			return false, errUIDenyAlways
		case "deny", "no":
			return false, nil
		default:
			return false, fmt.Errorf("unsupported UI approval action %q", resp.ActionID)
		}
	}

	if a.approve == nil {
		return false, fmt.Errorf("approval function not configured")
	}
	return a.approve(ctx, tc.Name, tc.Input)
}

func truncateUIInput(input json.RawMessage) string {
	s := string(input)
	if len(s) <= maxUIRequestInputBytes {
		return s
	}
	return s[:maxUIRequestInputBytes] + "...(truncated)"
}

func (a *Agent) toolError(tc ToolUseBlock, msg string) toolResult {
	return toolResult{
		content: msg,
		isError: true,
		event: TurnEvent{
			Type: "tool_result",
			ToolResult: &ToolResultEvent{
				ID:      tc.ID,
				Name:    tc.Name,
				Content: msg,
				IsError: true,
			},
		},
	}
}

func makeResultEvent(id, name string, res ToolResult) TurnEvent {
	return TurnEvent{
		Type: "tool_result",
		ToolResult: &ToolResultEvent{
			ID:             id,
			Name:           name,
			Content:        res.Content,
			DisplayContent: res.DisplayContent,
			IsError:        res.IsError,
		},
	}
}

func (a *Agent) makeDoneEvent(inputTokens, outputTokens int) TurnEvent {
	return TurnEvent{
		Type:         "done",
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}
}
