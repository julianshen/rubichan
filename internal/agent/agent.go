package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
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

// Agent orchestrates the conversation loop between the user, LLM, and tools.
type Agent struct {
	provider     provider.LLMProvider
	tools        *tools.Registry
	conversation *Conversation
	context      *ContextManager
	approve      ApprovalFunc
	model        string
	maxTurns     int
}

// New creates a new Agent with the given provider, tool registry, approval
// function, and configuration.
func New(p provider.LLMProvider, t *tools.Registry, approve ApprovalFunc, cfg *config.Config) *Agent {
	systemPrompt := buildSystemPrompt(cfg)
	return &Agent{
		provider:     p,
		tools:        t,
		conversation: NewConversation(systemPrompt),
		context:      NewContextManager(cfg.Agent.ContextBudget),
		approve:      approve,
		model:        cfg.Provider.Model,
		maxTurns:     cfg.Agent.MaxTurns,
	}
}

// buildSystemPrompt constructs the system prompt from configuration.
func buildSystemPrompt(_ *config.Config) string {
	return "You are a helpful AI coding assistant. You can read and write files, " +
		"execute shell commands, and help with software development tasks."
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

// runLoop recursively processes LLM responses and tool calls.
func (a *Agent) runLoop(ctx context.Context, ch chan<- TurnEvent, turnCount int) {
	if turnCount >= a.maxTurns {
		ch <- TurnEvent{Type: "error", Error: fmt.Errorf("max turns (%d) exceeded", a.maxTurns)}
		ch <- TurnEvent{Type: "done"}
		return
	}

	req := provider.CompletionRequest{
		Model:     a.model,
		System:    a.conversation.SystemPrompt(),
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
		ch <- TurnEvent{
			Type: "tool_call",
			ToolCall: &ToolCallEvent{
				ID:    tc.ID,
				Name:  tc.Name,
				Input: tc.Input,
			},
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

	// Recurse for the next turn after tool results
	a.runLoop(ctx, ch, turnCount+1)
}
