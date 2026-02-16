package agent

import (
	"context"
	"encoding/json"

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
