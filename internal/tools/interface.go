package tools

import (
	"context"
	"encoding/json"
)

// Tool defines the interface for a tool that can be used by the AI agent.
type Tool interface {
	Name() string
	Description() string
	InputSchema() json.RawMessage
	Execute(ctx context.Context, input json.RawMessage) (ToolResult, error)
}

// ToolResult represents the result of executing a tool.
type ToolResult struct {
	Content        string // sent to LLM conversation history
	DisplayContent string // shown to user; falls back to Content if empty
	IsError        bool
}

// Display returns the content intended for user display. It returns
// DisplayContent when set, otherwise falls back to Content.
func (r ToolResult) Display() string {
	if r.DisplayContent != "" {
		return r.DisplayContent
	}
	return r.Content
}
