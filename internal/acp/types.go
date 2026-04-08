// Package acp implements the Agent Client Protocol (ACP) message types.
// ACP is a standardized JSON-RPC 2.0 protocol for agent-editor communication.
package acp

import "encoding/json"

// Request represents a JSON-RPC 2.0 request message.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`          // Always "2.0"
	ID      interface{}     `json:"id,omitempty"`     // Request ID (number or string), omitted for notifications
	Method  string          `json:"method"`           // Method name
	Params  json.RawMessage `json:"params,omitempty"` // Method parameters
}

// Response represents a JSON-RPC 2.0 response message.
type Response struct {
	JSONRPC string           `json:"jsonrpc"`          // Always "2.0"
	ID      interface{}      `json:"id,omitempty"`     // Request ID (matches request)
	Result  *json.RawMessage `json:"result,omitempty"` // Result of successful call
	Error   *RPCError        `json:"error,omitempty"`  // Error object (present if error occurred)
}

// RPCError represents a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int             `json:"code"`           // Error code
	Message string          `json:"message"`        // Error message
	Data    json.RawMessage `json:"data,omitempty"` // Additional error data
}

// Notification represents a JSON-RPC 2.0 notification (request without ID).
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`          // Always "2.0"
	Method  string          `json:"method"`           // Method name
	Params  json.RawMessage `json:"params,omitempty"` // Method parameters
}

// Tool represents a tool capability that can be invoked by the agent.
type Tool struct {
	Name        string          `json:"name"`                  // Tool name (e.g., "file.read")
	Description string          `json:"description"`           // Human-readable description
	InputSchema json.RawMessage `json:"inputSchema,omitempty"` // JSON Schema for input parameters
}

// ToolCapability represents a tool capability in the capability registry.
type ToolCapability struct {
	Tool Tool `json:"tool"`
}

// Skill represents a skill that can be registered and invoked.
type Skill struct {
	Name        string          `json:"name"`                  // Skill name
	Manifest    json.RawMessage `json:"manifest,omitempty"`    // Skill manifest (metadata)
	Permissions []string        `json:"permissions,omitempty"` // Required permissions (e.g., "file:read")
}

// SkillCapability represents a skill capability in the capability registry.
type SkillCapability struct {
	Skill Skill `json:"skill"`
}

// SecurityVerdict represents a security finding or assessment result.
type SecurityVerdict struct {
	ID         string  `json:"id"`                   // Unique verdict identifier
	Status     string  `json:"status"`               // Status (e.g., "flagged", "approved", "resolved")
	Severity   string  `json:"severity"`             // Severity level (e.g., "high", "medium", "low")
	Message    string  `json:"message"`              // Human-readable message
	Confidence float64 `json:"confidence,omitempty"` // Confidence score (0.0-1.0)
}

// SecurityCapability represents a security capability in the capability registry.
type SecurityCapability struct {
	Verdicts []SecurityVerdict `json:"verdicts,omitempty"`
}

// ClientInfo describes the client connecting to the agent.
type ClientInfo struct {
	Name    string `json:"name"`              // Client name
	Version string `json:"version,omitempty"` // Client version
}

// ServerInfo describes the agent server.
type ServerInfo struct {
	Name    string `json:"name"`              // Server name (e.g., "rubichan")
	Version string `json:"version,omitempty"` // Server version
}

// InitializeRequest represents the initialize handshake request from client.
type InitializeRequest struct {
	JSONRPC string           `json:"jsonrpc"` // Always "2.0"
	ID      interface{}      `json:"id"`      // Request ID
	Method  string           `json:"method"`  // Always "initialize"
	Params  InitializeParams `json:"params"`
}

// InitializeParams are parameters for the initialize request.
type InitializeParams struct {
	ClientInfo ClientInfo `json:"clientInfo"`
}

// InitializeResponse represents the initialize handshake response from server.
type InitializeResponse struct {
	JSONRPC string           `json:"jsonrpc"` // Always "2.0"
	ID      interface{}      `json:"id"`      // Matches request ID
	Result  InitializeResult `json:"result"`
	Error   *RPCError        `json:"error,omitempty"`
}

// InitializeResult contains the server's initialization response data.
type InitializeResult struct {
	ServerInfo   ServerInfo             `json:"serverInfo"`
	Capabilities map[string]interface{} `json:"capabilities,omitempty"`
}

// CapabilityDefinition represents a single capability entry in the registry.
type CapabilityDefinition struct {
	Name       string          `json:"name"`                 // Capability name
	Type       string          `json:"type"`                 // Type (e.g., "tool", "skill", "security")
	Definition json.RawMessage `json:"definition,omitempty"` // Capability-specific definition
}

// RPC Methods
const (
	// Tool methods
	MethodListTools = "tools/list"
	MethodCallTool  = "tools/call"

	// Resource methods
	MethodListResources = "resources/list"
	MethodReadResource  = "resources/read"

	// Sampling (LLM) methods
	MethodSampling = "sampling/createMessage"

	// Prompt methods
	MethodListPrompts = "prompts/list"
	MethodCallPrompt  = "prompts/call"

	// Notification methods (server → client, no response expected)
	MethodNotificationProgress = "notifications/progress"
	MethodNotificationLog      = "notifications/log"

	// Rubichan-specific extensions
	MethodListSkills         = "skills/list"
	MethodCallSkill          = "skills/call"
	MethodGetSecurityVerdict = "security/getVerdict"
)

// Error Codes (JSON-RPC standard + custom)
const (
	ErrorCodeParseError       = -32700
	ErrorCodeInvalidRequest   = -32600
	ErrorCodeMethodNotFound   = -32601
	ErrorCodeInvalidParams    = -32602
	ErrorCodeInternalError    = -32603
	ErrorCodeServerError      = -32000
	ErrorCodeToolNotFound     = -32100
	ErrorCodeSkillNotFound    = -32101
	ErrorCodePermissionDenied = -32102
)
