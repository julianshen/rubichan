package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/tools"
)

// ApprovalFunc is called before executing a tool to get user approval.
// Returns true if the tool execution is approved.
type ApprovalFunc func(ctx context.Context, tool string, input json.RawMessage) (bool, error)

// TurnEvent represents a streaming event emitted during an agent turn.
type TurnEvent struct {
	Type       string           // "text_delta", "tool_call", "tool_result", "error", "done"
	Text       string           // text content for text_delta events
	ToolCall   *ToolCallEvent   // populated for tool_call events
	ToolResult *ToolResultEvent // populated for tool_result events
	Error      error            // populated for error events
}

// ToolCallEvent contains details about a tool being called.
type ToolCallEvent struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ToolResultEvent contains details about a tool execution result.
type ToolResultEvent struct {
	ID      string
	Name    string
	Content string
	IsError bool
}

// AgentOption is a functional option for configuring an Agent.
type AgentOption func(*Agent)

// WithSkillRuntime attaches a skill runtime to the agent, enabling hook
// dispatch and prompt fragment injection.
func WithSkillRuntime(rt *skills.Runtime) AgentOption {
	return func(a *Agent) {
		a.skillRuntime = rt
	}
}

// Agent orchestrates the conversation loop between the user, LLM, and tools.
type Agent struct {
	provider     provider.LLMProvider
	tools        *tools.Registry
	conversation *Conversation
	context      *ContextManager
	approve      ApprovalFunc
	model        string
	maxTurns     int
	skillRuntime *skills.Runtime
}

// New creates a new Agent with the given provider, tool registry, approval
// function, and configuration. Optional AgentOption values can be provided
// to attach a skill runtime.
func New(p provider.LLMProvider, t *tools.Registry, approve ApprovalFunc, cfg *config.Config, opts ...AgentOption) *Agent {
	systemPrompt := buildSystemPrompt(cfg)
	a := &Agent{
		provider:     p,
		tools:        t,
		conversation: NewConversation(systemPrompt),
		context:      NewContextManager(cfg.Agent.ContextBudget),
		approve:      approve,
		model:        cfg.Provider.Model,
		maxTurns:     cfg.Agent.MaxTurns,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// buildSystemPrompt constructs the system prompt from configuration.
func buildSystemPrompt(_ *config.Config) string {
	return "You are a helpful AI coding assistant. You can read and write files, " +
		"execute shell commands, and help with software development tasks."
}

// ClearConversation removes all messages from the conversation history,
// preserving the system prompt.
func (a *Agent) ClearConversation() {
	a.conversation.Clear()
}

// SetModel changes the model used for LLM completions.
func (a *Agent) SetModel(model string) {
	a.model = model
}

// Turn initiates a new agent turn with the given user message. It returns a
// channel of TurnEvent that streams events as the agent processes the turn.
func (a *Agent) Turn(ctx context.Context, userMessage string) (<-chan TurnEvent, error) {
	a.conversation.AddUser(userMessage)
	a.context.Truncate(a.conversation)

	ch := make(chan TurnEvent, 64)
	go func() {
		defer close(ch)
		a.runLoop(ctx, ch, 0)
	}()
	return ch, nil
}

// buildSystemPromptWithFragments returns the base system prompt with any
// skill prompt fragments appended.
func (a *Agent) buildSystemPromptWithFragments() string {
	base := a.conversation.SystemPrompt()
	if a.skillRuntime == nil {
		return base
	}

	fragments := a.skillRuntime.GetPromptFragments()
	if len(fragments) == 0 {
		return base
	}

	result := base
	for _, f := range fragments {
		if f.SystemPromptFile != "" {
			result += "\n\n" + f.SystemPromptFile
		}
	}
	return result
}

// dispatchHook dispatches a hook event via the skill runtime. If no runtime is
// configured, it returns nil. This is safe to call even when skillRuntime is nil.
func (a *Agent) dispatchHook(event skills.HookEvent) (*skills.HookResult, error) {
	if a.skillRuntime == nil {
		return nil, nil
	}
	return a.skillRuntime.DispatchHook(event)
}

// runLoop iteratively processes LLM responses and tool calls.
func (a *Agent) runLoop(ctx context.Context, ch chan<- TurnEvent, turnCount int) {
	for ; turnCount < a.maxTurns; turnCount++ {
	if ctx.Err() != nil {
		ch <- TurnEvent{Type: "error", Error: ctx.Err()}
		ch <- TurnEvent{Type: "done"}
		return
	}

	// Build the system prompt, injecting prompt fragments from skills.
	systemPrompt := a.buildSystemPromptWithFragments()

	req := provider.CompletionRequest{
		Model:     a.model,
		System:    systemPrompt,
		Messages:  a.conversation.Messages(),
		Tools:     a.tools.All(),
		MaxTokens: 4096,
	}

	stream, err := a.provider.Stream(ctx, req)
	if err != nil {
		ch <- TurnEvent{Type: "error", Error: fmt.Errorf("provider stream: %w", err)}
		ch <- TurnEvent{Type: "done"}
		return
	}

	// Accumulate assistant content blocks and track tool calls
	var blocks []provider.ContentBlock
	var pendingTools []provider.ToolUseBlock
	var currentTextBuf string
	var currentTool *provider.ToolUseBlock
	var toolInputBuf string

	finalizeTool := func() {
		if currentTool != nil {
			currentTool.Input = json.RawMessage(toolInputBuf)
			pendingTools = append(pendingTools, *currentTool)
			blocks = append(blocks, provider.ContentBlock{
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
			blocks = append(blocks, provider.ContentBlock{
				Type: "text",
				Text: currentTextBuf,
			})
			currentTextBuf = ""
		}
	}

	for event := range stream {
		switch event.Type {
		case "text_delta":
			if currentTool != nil {
				// Accumulating tool input JSON fragments
				toolInputBuf += event.Text
			} else {
				// Regular text content
				currentTextBuf += event.Text
				ch <- TurnEvent{Type: "text_delta", Text: event.Text}
			}

		case "tool_use":
			// Finalize any pending text block
			finalizeText()
			// Finalize any previous tool
			finalizeTool()
			// Start new tool accumulation
			currentTool = &provider.ToolUseBlock{
				ID:   event.ToolUse.ID,
				Name: event.ToolUse.Name,
			}

		case "error":
			ch <- TurnEvent{Type: "error", Error: event.Error}

		case "stop":
			// Will be handled after the loop
		}
	}

	// Finalize any remaining text or tool
	finalizeText()
	finalizeTool()

	// Add assistant message with accumulated blocks
	if len(blocks) > 0 {
		a.conversation.AddAssistant(blocks)
	}

	// If no pending tool calls, we're done
	if len(pendingTools) == 0 {
		ch <- TurnEvent{Type: "done"}
		return
	}

	// Execute tool calls
	for _, tc := range pendingTools {
		if ctx.Err() != nil {
			ch <- TurnEvent{Type: "error", Error: ctx.Err()}
			ch <- TurnEvent{Type: "done"}
			return
		}

		ch <- TurnEvent{
			Type: "tool_call",
			ToolCall: &ToolCallEvent{
				ID:    tc.ID,
				Name:  tc.Name,
				Input: tc.Input,
			},
		}

		// Dispatch HookOnBeforeToolCall hook. If cancelled, skip execution.
		hookResult, hookErr := a.dispatchHook(skills.HookEvent{
			Phase: skills.HookOnBeforeToolCall,
			Data: map[string]any{
				"tool_name": tc.Name,
				"input":     string(tc.Input),
			},
			Ctx: ctx,
		})
		if hookErr != nil {
			result := fmt.Sprintf("hook error: %s", hookErr)
			a.conversation.AddToolResult(tc.ID, result, true)
			ch <- TurnEvent{
				Type: "tool_result",
				ToolResult: &ToolResultEvent{
					ID:      tc.ID,
					Name:    tc.Name,
					Content: result,
					IsError: true,
				},
			}
			continue
		}
		if hookResult != nil && hookResult.Cancel {
			result := "tool call cancelled by skill"
			a.conversation.AddToolResult(tc.ID, result, true)
			ch <- TurnEvent{
				Type: "tool_result",
				ToolResult: &ToolResultEvent{
					ID:      tc.ID,
					Name:    tc.Name,
					Content: result,
					IsError: true,
				},
			}
			continue
		}

		// Check approval
		approved, approvalErr := a.approve(ctx, tc.Name, tc.Input)
		if approvalErr != nil {
			result := fmt.Sprintf("approval error: %s", approvalErr)
			a.conversation.AddToolResult(tc.ID, result, true)
			ch <- TurnEvent{
				Type: "tool_result",
				ToolResult: &ToolResultEvent{
					ID:      tc.ID,
					Name:    tc.Name,
					Content: result,
					IsError: true,
				},
			}
			continue
		}

		if !approved {
			result := "tool call denied by user"
			a.conversation.AddToolResult(tc.ID, result, true)
			ch <- TurnEvent{
				Type: "tool_result",
				ToolResult: &ToolResultEvent{
					ID:      tc.ID,
					Name:    tc.Name,
					Content: result,
					IsError: true,
				},
			}
			continue
		}

		// Look up and execute the tool
		tool, found := a.tools.Get(tc.Name)
		if !found {
			result := fmt.Sprintf("unknown tool: %s", tc.Name)
			a.conversation.AddToolResult(tc.ID, result, true)
			ch <- TurnEvent{
				Type: "tool_result",
				ToolResult: &ToolResultEvent{
					ID:      tc.ID,
					Name:    tc.Name,
					Content: result,
					IsError: true,
				},
			}
			continue
		}

		toolResult, execErr := tool.Execute(ctx, tc.Input)
		if execErr != nil {
			result := fmt.Sprintf("tool execution error: %s", execErr)
			a.conversation.AddToolResult(tc.ID, result, true)
			ch <- TurnEvent{
				Type: "tool_result",
				ToolResult: &ToolResultEvent{
					ID:      tc.ID,
					Name:    tc.Name,
					Content: result,
					IsError: true,
				},
			}
			continue
		}

		// Dispatch HookOnAfterToolResult hook. If modified, use the new content.
		afterResult, afterErr := a.dispatchHook(skills.HookEvent{
			Phase: skills.HookOnAfterToolResult,
			Data: map[string]any{
				"tool_name": tc.Name,
				"content":   toolResult.Content,
				"is_error":  toolResult.IsError,
			},
			Ctx: ctx,
		})
		if afterErr == nil && afterResult != nil && afterResult.Modified != nil {
			if modContent, ok := afterResult.Modified["content"].(string); ok {
				toolResult.Content = modContent
			}
		}

		a.conversation.AddToolResult(tc.ID, toolResult.Content, toolResult.IsError)
		ch <- TurnEvent{
			Type: "tool_result",
			ToolResult: &ToolResultEvent{
				ID:      tc.ID,
				Name:    tc.Name,
				Content: toolResult.Content,
				IsError: toolResult.IsError,
			},
		}
	}

	// Continue to the next turn after tool results.
	}

	// Reached max turns.
	ch <- TurnEvent{Type: "error", Error: fmt.Errorf("max turns (%d) exceeded", a.maxTurns)}
	ch <- TurnEvent{Type: "done"}
}
