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
	Content string
	IsError bool
}
