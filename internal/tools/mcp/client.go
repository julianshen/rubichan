package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
)

// MCPTool describes a tool discovered from an MCP server.
type MCPTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ToolResult is the result of calling an MCP tool.
type ToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ContentBlock is a content block in a tool result.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Client manages a single MCP server connection.
type Client struct {
	name       string
	transport  Transport
	nextID     atomic.Int64
	serverName string
}

// NewClient creates a new MCP client.
func NewClient(name string, transport Transport) *Client {
	return &Client{
		name:      name,
		transport: transport,
	}
}

// ServerName returns the server's self-reported name after Initialize.
func (c *Client) ServerName() string {
	return c.serverName
}

// Initialize performs the MCP protocol handshake.
func (c *Client) Initialize(ctx context.Context) error {
	id := c.nextID.Add(1)

	params, _ := json.Marshal(map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "rubichan",
			"version": "1.0.0",
		},
	})

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "initialize",
		Params:  params,
	}

	if err := c.transport.Send(ctx, req); err != nil {
		return fmt.Errorf("send initialize: %w", err)
	}

	resp, err := c.receiveResponse(ctx, id)
	if err != nil {
		return fmt.Errorf("receive initialize response: %w", err)
	}

	if resp.Error != nil {
		return resp.Error
	}

	var initResult struct {
		ServerInfo struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(resp.Result, &initResult); err != nil {
		return fmt.Errorf("parse initialize result: %w", err)
	}

	c.serverName = initResult.ServerInfo.Name

	// Per MCP spec, client MUST send notifications/initialized after successful handshake.
	notification := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	if err := c.transport.Send(ctx, notification); err != nil {
		return fmt.Errorf("send notifications/initialized: %w", err)
	}

	return nil
}

// ListTools discovers available tools from the MCP server.
func (c *Client) ListTools(ctx context.Context) ([]MCPTool, error) {
	id := c.nextID.Add(1)

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "tools/list",
	}

	if err := c.transport.Send(ctx, req); err != nil {
		return nil, fmt.Errorf("send tools/list: %w", err)
	}

	resp, err := c.receiveResponse(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("receive tools/list response: %w", err)
	}

	if resp.Error != nil {
		return nil, resp.Error
	}

	var listResult struct {
		Tools []MCPTool `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &listResult); err != nil {
		return nil, fmt.Errorf("parse tools/list result: %w", err)
	}

	return listResult.Tools, nil
}

// CallTool executes a tool on the MCP server.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (*ToolResult, error) {
	id := c.nextID.Add(1)

	// MCP spec requires "arguments" to be an object, never null.
	if args == nil {
		args = map[string]any{}
	}

	params, err := json.Marshal(map[string]any{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal tools/call params: %w", err)
	}

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "tools/call",
		Params:  params,
	}

	if err := c.transport.Send(ctx, req); err != nil {
		return nil, fmt.Errorf("send tools/call: %w", err)
	}

	resp, err := c.receiveResponse(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("receive tools/call response: %w", err)
	}

	if resp.Error != nil {
		return nil, resp.Error
	}

	var result ToolResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("parse tools/call result: %w", err)
	}

	return &result, nil
}

// receiveResponse reads from the transport, skipping server-sent notifications
// (messages without an ID), and returns the first response whose ID matches
// the expected request ID.
//
// This method is NOT safe for concurrent use. The Client serializes requests
// through its public API methods (Initialize, ListTools, CallTool), and each
// waits for its response before returning. Do not call receiveResponse from
// multiple goroutines.
func (c *Client) receiveResponse(ctx context.Context, expectedID int64) (*jsonRPCResponse, error) {
	for {
		var resp jsonRPCResponse
		if err := c.transport.Receive(ctx, &resp); err != nil {
			return nil, err
		}

		// Notifications have no ID — skip them.
		if resp.ID == nil {
			continue
		}

		// JSON numbers unmarshal as float64. Compare via float64 for robustness.
		if id, ok := resp.ID.(float64); ok && int64(id) == expectedID {
			return &resp, nil
		}

		// Non-matching ID — skip (could be a stale or out-of-order response).
		continue
	}
}

// Close shuts down the client and its transport.
func (c *Client) Close() error {
	return c.transport.Close()
}
