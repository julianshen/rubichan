package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/internal/tools"
)

// wrappedTool adapts an MCP tool to the tools.Tool interface.
type wrappedTool struct {
	serverName string
	client     *Client
	mcpTool    MCPTool
}

// compile-time check
var _ tools.Tool = (*wrappedTool)(nil)

// WrapTool creates a tools.Tool adapter for an MCP tool.
func WrapTool(serverName string, client *Client, mcpTool MCPTool) tools.Tool {
	return &wrappedTool{
		serverName: serverName,
		client:     client,
		mcpTool:    mcpTool,
	}
}

func (w *wrappedTool) Name() string {
	return fmt.Sprintf("mcp_%s_%s", w.serverName, w.mcpTool.Name)
}

func (w *wrappedTool) Description() string {
	return w.mcpTool.Description
}

func (w *wrappedTool) InputSchema() json.RawMessage {
	if len(w.mcpTool.InputSchema) > 0 {
		return w.mcpTool.InputSchema
	}
	return json.RawMessage(`{"type":"object"}`)
}

func (w *wrappedTool) Execute(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	var args map[string]any
	if len(input) > 0 {
		if err := json.Unmarshal(input, &args); err != nil {
			return tools.ToolResult{IsError: true, Content: err.Error()}, nil
		}
	}

	result, err := w.client.CallTool(ctx, w.mcpTool.Name, args)
	if err != nil {
		// Transport/protocol errors (JSON-RPC errors, network failures) are
		// propagated as Go errors so the caller can retry or surface them.
		// Only MCP tool-level failures (result.IsError) are returned as ToolResult.
		return tools.ToolResult{}, fmt.Errorf("mcp call %q: %w", w.mcpTool.Name, err)
	}

	// Concatenate text content blocks
	var parts []string
	for _, block := range result.Content {
		if block.Type == "text" {
			parts = append(parts, block.Text)
		}
	}

	return tools.ToolResult{
		Content: strings.Join(parts, "\n"),
		IsError: result.IsError,
	}, nil
}
