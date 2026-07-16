package agentsdk

import (
	"context"
	"errors"
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
			eventType := "error"
			var classifier ProviderErrorClassifier
			if errors.As(err, &classifier) && classifier.ProviderErrorKind() == ProviderErrContextOverflow {
				eventType = "context_overflow"
			}
			ch <- TurnEvent{Type: eventType, Error: fmt.Errorf("provider stream: %w", err)}
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
// tool calls via StreamAccumulator.
func (a *Agent) consumeStream(
	ctx context.Context,
	ch chan<- TurnEvent,
	stream <-chan StreamEvent,
	totalInput, totalOutput *int,
) streamResult {
	acc := NewStreamAccumulator()

	var hadError bool
	for event := range stream {
		*totalInput += event.InputTokens
		*totalOutput += event.OutputTokens

		switch event.Type {
		case EventMessageStart:
			ch <- TurnEvent{Type: EventMessageStart, Model: event.Model, MessageID: event.MessageID}
		case EventTextDelta:
			// During tool accumulation, text deltas carry JSON input
			// for the tool call, not user-visible text.
			if !acc.AddText(event.Text) {
				ch <- TurnEvent{Type: EventTextDelta, Text: event.Text}
			}
		case EventInputJsonDelta:
			if acc.AddToolInput(event.Text) {
				ch <- TurnEvent{Type: EventInputJsonDelta, Text: event.Text}
			}
		case EventToolUse:
			if event.ToolUse == nil {
				acc.Finish()
				ch <- TurnEvent{Type: "error", Error: fmt.Errorf("provider sent tool_use event with nil ToolUse")}
				hadError = true
				continue
			}
			acc.StartTool(*event.ToolUse)
		case EventError:
			a.logger.Error("stream error: %v", event.Error)
			ch <- TurnEvent{Type: "error", Error: fmt.Errorf("provider stream event: %w", event.Error)}
			// Discard all accumulated state to avoid processing
			// data from a corrupt/partial stream.
			acc.Reset()
			hadError = true
		case EventStop:
			// handled after loop
		}
	}

	if !hadError {
		acc.Finish()
	}

	return streamResult{
		blocks:       acc.Blocks(),
		pendingTools: acc.PendingTools(),
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

		ch <- MakeToolCallEvent(tc)

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
	// Run the shared approval flow when any approval mechanism is
	// configured. With nothing configured the SDK executes directly —
	// approval is opt-in for embedders.
	if a.approvalChecker != nil || a.approve != nil || a.uiRequestHandler != nil {
		flow := &ApprovalFlow{
			Checker:   a.approvalChecker,
			Approve:   a.approve,
			UIHandler: a.uiRequestHandler,
			Emit:      func(ev TurnEvent) { sendEvent(ctx, ch, ev) },
		}
		if out := flow.Decide(ctx, tc); !out.Approved {
			if out.Err != nil {
				a.logger.Error("approval failure for tool %s: %v", tc.Name, out.Err)
			}
			return a.toolError(tc, out.Message)
		}
	}

	// Dispatch through the shared execution core: registry lookup with
	// did-you-mean suggestions, streaming-aware execution, error wrapping.
	emit := MakeToolProgressEmitter(tc.ID, tc.Name, func(ev TurnEvent) { sendEvent(ctx, ch, ev) })
	out := ExecuteTool(ctx, a.tools, tc.Name, tc.Input, emit)
	return toolResult{
		content: out.Content,
		isError: out.IsError,
		event:   MakeToolResultEvent(tc.ID, tc.Name, out.Content, out.DisplayContent, out.IsError),
	}
}

func (a *Agent) toolError(tc ToolUseBlock, msg string) toolResult {
	return toolResult{
		content: msg,
		isError: true,
		event:   MakeToolResultEvent(tc.ID, tc.Name, msg, "", true),
	}
}

func (a *Agent) makeDoneEvent(inputTokens, outputTokens int) TurnEvent {
	return TurnEvent{
		Type:         "done",
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}
}
