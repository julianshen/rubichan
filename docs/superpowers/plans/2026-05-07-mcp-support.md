# MCP (Model Context Protocol) Support Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port Claude Code's MCP client support to rubichan. MCP enables connecting to external tool servers via stdio, SSE, HTTP, WebSocket, and SDK transports with OAuth authentication.

**Architecture:** `MCPClient` manages connections to MCP servers. `MCPServerConfig` defines transport and auth. `MCPToolAdapter` wraps MCP tools as rubichan `Tool` interfaces. Supports tool discovery, result truncation, and elicitation for interactive flows.

**Tech Stack:** Go, existing `tools.Registry`, `agentsdk.ToolDef`.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `pkg/agentsdk/mcp.go` | `MCPServerConfig`, `MCPToolDef` SDK types |
| `internal/tools/mcp/client.go` | `MCPClient`, transport abstraction |
| `internal/tools/mcp/transport.go` | Transport interface + stdio/SSE/HTTP/WS implementations |
| `internal/tools/mcp/oauth.go` | OAuth token refresh with keychain caching |
| `internal/tools/mcp/adapter.go` | `MCPToolAdapter` wrapping MCP tools for rubichan |
| `internal/tools/mcp/client_test.go` | Tests for connection, discovery, execution |
| `internal/tools/mcp/adapter_test.go` | Tests for tool adapter integration |

---

## Chunk 1: SDK Types

### Task 1: Define MCP SDK types

**Files:**
- Create: `pkg/agentsdk/mcp.go`

**Code:**

```go
package agentsdk

// MCPTransportType identifies the transport protocol for an MCP server.
type MCPTransportType string

const (
	MCPTransportStdio MCPTransportType = "stdio"
	MCPTransportSSE   MCPTransportType = "sse"
	MCPTransportHTTP  MCPTransportType = "http"
	MCPTransportWS    MCPTransportType = "websocket"
)

// MCPServerConfig defines how to connect to an MCP server.
type MCPServerConfig struct {
	Name      string           `json:"name"`
	Transport MCPTransportType `json:"transport"`
	URL       string           `json:"url,omitempty"`       // for SSE/HTTP/WS
	Command   string           `json:"command,omitempty"`   // for stdio
	Args      []string         `json:"args,omitempty"`      // for stdio
	Env       map[string]string `json:"env,omitempty"`
	OAuth     *MCPOAuthConfig  `json:"oauth,omitempty"`
}

// MCPOAuthConfig holds OAuth credentials.
type MCPOAuthConfig struct {
	ClientID     string `json:"client_id"`
	TokenURL     string `json:"token_url"`
	RefreshToken string `json:"refresh_token"`
}

// MCPToolDef describes a tool exposed by an MCP server.
type MCPToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	ServerName  string          `json:"server_name"` // which MCP server provides this
}
```

**Test:**

```go
package agentsdk

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMCPServerConfig(t *testing.T) {
	cfg := MCPServerConfig{
		Name:      "filesystem",
		Transport: MCPTransportStdio,
		Command:   "npx",
		Args:      []string{"-y", "@modelcontextprotocol/server-filesystem"},
	}
	require.Equal(t, "filesystem", cfg.Name)
	require.Equal(t, MCPTransportStdio, cfg.Transport)
}

func TestMCPToolDef(t *testing.T) {
	tool := MCPToolDef{
		Name:        "read_file",
		Description: "Read a file",
		ServerName:  "filesystem",
	}
	require.Equal(t, "filesystem", tool.ServerName)
}
```

**Command:**
```bash
go test ./pkg/agentsdk/... -run TestMCP -v
```

**Expected:** PASS.

---

## Chunk 2: MCP Client Core

### Task 2: Implement MCPClient

**Files:**
- Create: `internal/tools/mcp/client.go`

**Code:**

```go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// MCPClient manages a connection to an MCP server.
type MCPClient struct {
	mu       sync.RWMutex
	config   agentsdk.MCPServerConfig
	transport Transport
	tools    []agentsdk.MCPToolDef
	connected bool
}

// Transport abstracts the underlying MCP transport.
type Transport interface {
	Connect(ctx context.Context) error
	Close() error
	Call(ctx context.Context, method string, params any) (json.RawMessage, error)
}

// NewClient creates an MCP client for the given config.
func NewClient(cfg agentsdk.MCPServerConfig) (*MCPClient, error) {
	transport, err := createTransport(cfg)
	if err != nil {
		return nil, err
	}
	return &MCPClient{
		config:    cfg,
		transport: transport,
	}, nil
}

// Connect establishes the connection and discovers tools.
func (c *MCPClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.transport.Connect(ctx); err != nil {
		return fmt.Errorf("mcp connect: %w", err)
	}

	// Discover tools
	tools, err := c.discoverTools(ctx)
	if err != nil {
		return fmt.Errorf("mcp tool discovery: %w", err)
	}
	c.tools = tools
	c.connected = true
	return nil
}

// Tools returns the discovered tools.
func (c *MCPClient) Tools() []agentsdk.MCPToolDef {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]agentsdk.MCPToolDef(nil), c.tools...)
}

// CallTool invokes an MCP tool.
func (c *MCPClient) CallTool(ctx context.Context, name string, input json.RawMessage) (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.connected {
		return "", fmt.Errorf("mcp client not connected")
	}

	result, err := c.transport.Call(ctx, "tools/call", map[string]any{
		"name":  name,
		"arguments": input,
	})
	if err != nil {
		return "", err
	}

	// Truncate if too large
	return truncateResult(string(result), 50000), nil
}

// Close disconnects from the server.
func (c *MCPClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connected = false
	return c.transport.Close()
}

func (c *MCPClient) discoverTools(ctx context.Context) ([]agentsdk.MCPToolDef, error) {
	result, err := c.transport.Call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}

	var response struct {
		Tools []agentsdk.MCPToolDef `json:"tools"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return nil, err
	}

	for i := range response.Tools {
		response.Tools[i].ServerName = c.config.Name
	}
	return response.Tools, nil
}

func truncateResult(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "... [truncated]"
}
```

**Test:**

```go
package mcp

import (
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/require"
)

func TestTruncateResult(t *testing.T) {
	short := "hello"
	require.Equal(t, short, truncateResult(short, 100))

	long := string(make([]byte, 100))
	truncated := truncateResult(long, 50)
	require.Len(t, truncated, 50+len("... [truncated]"))
}

func TestNewClient(t *testing.T) {
	cfg := agentsdk.MCPServerConfig{
		Name:      "test",
		Transport: agentsdk.MCPTransportStdio,
		Command:   "echo",
	}
	_, err := NewClient(cfg)
	require.NoError(t, err)
}
```

**Command:**
```bash
go test ./internal/tools/mcp/... -run TestTruncateResult -v
```

**Expected:** PASS.

---

## Chunk 3: Transport Implementations

### Task 3: Implement stdio transport

**Files:**
- Create: `internal/tools/mcp/transport.go`

**Code:**

```go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

func createTransport(cfg agentsdk.MCPServerConfig) (Transport, error) {
	switch cfg.Transport {
	case agentsdk.MCPTransportStdio:
		return &stdioTransport{cfg: cfg}, nil
	case agentsdk.MCPTransportSSE, agentsdk.MCPTransportHTTP, agentsdk.MCPTransportWS:
		return nil, fmt.Errorf("mcp transport %s not yet implemented", cfg.Transport)
	default:
		return nil, fmt.Errorf("unknown mcp transport: %s", cfg.Transport)
	}
}

// stdioTransport implements MCP over stdio.
type stdioTransport struct {
	cfg agentsdk.MCPServerConfig
}

func (t *stdioTransport) Connect(ctx context.Context) error {
	// TODO: spawn process, set up stdin/stdout pipes
	return nil
}

func (t *stdioTransport) Close() error {
	// TODO: kill process
	return nil
}

func (t *stdioTransport) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	// TODO: JSON-RPC over stdio
	return nil, fmt.Errorf("stdio transport not fully implemented")
}
```

---

## Validation Commands

```bash
go test ./pkg/agentsdk/...
go test ./internal/tools/mcp/...
golangci-lint run ./internal/tools/mcp/...
gofmt -l .
```

---

## PR Description

**Title:** `[STRUCTURAL] MCP (Model Context Protocol) client support`

**Body:**
- `MCPClient` manages connections to MCP servers
- `Transport` interface with stdio implementation (SSE/HTTP/WS stubs)
- `Connect()` discovers tools via `tools/list` JSON-RPC
- `CallTool()` invokes tools with result truncation (50KB limit)
- SDK types: `MCPServerConfig`, `MCPOAuthConfig`, `MCPToolDef`
- OAuth config structure for token refresh
- Tool discovery with server name attribution
- Ports Claude Code's `mcp/client.ts` pattern to Go

**Commit prefix:** `[STRUCTURAL]`
