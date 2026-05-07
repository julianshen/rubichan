package agentsdk

import (
	"encoding/json"
	"fmt"
)

// MCPTransportType identifies the transport protocol for an MCP server.
type MCPTransportType string

const (
	MCPTransportStdio MCPTransportType = "stdio"
	MCPTransportSSE   MCPTransportType = "sse"
	MCPTransportHTTP  MCPTransportType = "http"
	MCPTransportWS    MCPTransportType = "websocket"
)

// Valid returns true if the transport type is one of the supported constants.
func (t MCPTransportType) Valid() bool {
	switch t {
	case MCPTransportStdio, MCPTransportSSE, MCPTransportHTTP, MCPTransportWS:
		return true
	}
	return false
}

// MCPServerConfig defines how to connect to an MCP server.
type MCPServerConfig struct {
	Name      string            `json:"name"`
	Transport MCPTransportType  `json:"transport"`
	URL       string            `json:"url,omitempty"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	OAuth     *MCPOAuthConfig   `json:"oauth,omitempty"`
}

// Validate checks that the MCPServerConfig fields are consistent.
func (c *MCPServerConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("mcp server: name is required")
	}
	if !c.Transport.Valid() {
		return fmt.Errorf("mcp server %q: unknown transport %q", c.Name, c.Transport)
	}
	switch c.Transport {
	case MCPTransportStdio:
		if c.Command == "" {
			return fmt.Errorf("mcp server %q: command is required for stdio transport", c.Name)
		}
	case MCPTransportSSE, MCPTransportHTTP, MCPTransportWS:
		if c.URL == "" {
			return fmt.Errorf("mcp server %q: url is required for %s transport", c.Name, c.Transport)
		}
	}
	if c.OAuth != nil {
		if c.OAuth.ClientID == "" {
			return fmt.Errorf("mcp server %q: oauth client_id is required", c.Name)
		}
		if c.OAuth.TokenURL == "" {
			return fmt.Errorf("mcp server %q: oauth token_url is required", c.Name)
		}
	}
	return nil
}

// MCPOAuthConfig holds OAuth credentials.
type MCPOAuthConfig struct {
	ClientID     string `json:"client_id"`
	TokenURL     string `json:"token_url"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

// MCPToolDef describes a tool exposed by an MCP server.
type MCPToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
	ServerName  string          `json:"server_name"`
}
