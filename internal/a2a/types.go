// Package a2a implements the client and server sides of Google's A2A
// (Agent2Agent) protocol: HTTP + JSON-RPC 2.0, with SSE for streaming.
// It is independent of internal/acp (Anthropic's unrelated Agent Client
// Protocol, a persistent bidirectional stream) — the two protocols use
// different transports and this package declares its own JSON-RPC types
// rather than sharing acp's.
package a2a

import "encoding/json"

// AgentCard is the "business card" an A2A agent serves at
// /.well-known/agent.json, advertising what it can do.
type AgentCard struct {
	Name         string       `json:"name"`
	Description  string       `json:"description"`
	URL          string       `json:"url"`
	Skills       []Skill      `json:"skills"`
	Capabilities Capabilities `json:"capabilities"`
	Auth         []AuthScheme `json:"authentication,omitempty"`
}

// Skill describes one capability an agent advertises.
type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
}

// Capabilities describes optional protocol features an agent supports.
type Capabilities struct {
	Streaming         bool `json:"streaming,omitempty"`
	PushNotifications bool `json:"pushNotifications,omitempty"`
}

// AuthScheme describes one authentication scheme an agent accepts.
type AuthScheme struct {
	Scheme string `json:"scheme"` // e.g. "bearer"
}

// TaskState is one state in a Task's lifecycle.
type TaskState string

const (
	TaskSubmitted     TaskState = "submitted"
	TaskWorking       TaskState = "working"
	TaskInputRequired TaskState = "input-required"
	TaskAuthRequired  TaskState = "auth-required"
	TaskCompleted     TaskState = "completed"
	TaskFailed        TaskState = "failed"
	TaskCanceled      TaskState = "canceled"
	TaskRejected      TaskState = "rejected"
)

// Task is the unit of work in A2A: a request that moves through
// TaskState until it reaches a terminal state.
type Task struct {
	ID        string     `json:"id"`
	ContextID string     `json:"contextId"`
	State     TaskState  `json:"state"`
	Messages  []Message  `json:"history,omitempty"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
	Error     string     `json:"error,omitempty"`
}

// Message is one turn of communication, sent by "user" or "agent".
type Message struct {
	Role  string `json:"role"`
	Parts []Part `json:"parts"`
}

// Part is a tagged union over A2A's part kinds. Exactly one of Text,
// DataJSON, or (FileURL or FileBytes) is populated, selected by Kind.
type Part struct {
	Kind      string          `json:"kind"`
	Text      string          `json:"text,omitempty"`
	DataJSON  json.RawMessage `json:"data,omitempty"`
	FileURL   string          `json:"fileUrl,omitempty"`
	FileBytes []byte          `json:"fileBytes,omitempty"`
}

// Artifact is a named result produced by a Task.
type Artifact struct {
	Name  string `json:"name"`
	Parts []Part `json:"parts"`
}

// RPCRequest is a JSON-RPC 2.0 request envelope.
type RPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// RPCResponse is a JSON-RPC 2.0 response envelope.
type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
