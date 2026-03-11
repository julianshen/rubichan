package agentsdk

import (
	"context"
	"encoding/json"
	"fmt"
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
// This is the public SDK entry point for external consumers.
type Agent struct {
	provider        LLMProvider
	tools           *Registry
	config          AgentConfig
	conversation    *Conversation
	approve         ApprovalFunc
	approvalChecker ApprovalChecker
	logger          Logger
	turnMu          sync.Mutex
}

// NewAgent creates a new Agent with the given LLM provider and options.
func NewAgent(provider LLMProvider, opts ...Option) *Agent {
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

// Turn initiates a new agent turn with the given user message. It returns a
// channel of TurnEvent that streams events as the agent processes the turn.
// Concurrent calls are serialized.
func (a *Agent) Turn(ctx context.Context, userMessage string) (<-chan TurnEvent, error) {
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

		blocks, pendingTools, cancelled := a.consumeStream(ctx, ch, stream, &totalInputTokens, &totalOutputTokens)
		if cancelled {
			ch <- a.makeDoneEvent(totalInputTokens, totalOutputTokens)
			return
		}

		if len(blocks) > 0 {
			a.conversation.AddAssistant(blocks)
		}

		if len(pendingTools) == 0 {
			ch <- a.makeDoneEvent(totalInputTokens, totalOutputTokens)
			return
		}

		if cancelled := a.executeTools(ctx, ch, pendingTools); cancelled {
			ch <- TurnEvent{Type: "error", Error: ctx.Err()}
			ch <- a.makeDoneEvent(totalInputTokens, totalOutputTokens)
			return
		}
	}

	ch <- TurnEvent{Type: "error", Error: fmt.Errorf("max turns (%d) exceeded", maxTurns)}
	ch <- a.makeDoneEvent(totalInputTokens, totalOutputTokens)
}

// consumeStream reads the provider stream, accumulating content blocks and
// tool calls. Returns the accumulated blocks, pending tool calls, and whether
// the context was cancelled.
func (a *Agent) consumeStream(
	ctx context.Context,
	ch chan<- TurnEvent,
	stream <-chan StreamEvent,
	totalInput, totalOutput *int,
) ([]ContentBlock, []ToolUseBlock, bool) {
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

	for event := range stream {
		*totalInput += event.InputTokens
		*totalOutput += event.OutputTokens

		switch event.Type {
		case "text_delta":
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
			ch <- TurnEvent{Type: "error", Error: event.Error}
		case "stop":
			// handled after loop
		}
	}

	finalizeText()
	finalizeTool()

	return blocks, pendingTools, ctx.Err() != nil
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
			if a.approve == nil {
				return a.toolError(tc, "approval function not configured")
			}
			approved, err := a.approve(ctx, tc.Name, tc.Input)
			if err != nil {
				a.logger.Error("approval failure for tool %s: %v", tc.Name, err)
				return a.toolError(tc, "approval error")
			}
			if !approved {
				return a.toolError(tc, "tool call denied by user")
			}
		}
	} else if a.approve != nil {
		// No checker — always ask for approval.
		approved, err := a.approve(ctx, tc.Name, tc.Input)
		if err != nil {
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
