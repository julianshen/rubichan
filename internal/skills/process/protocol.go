// Package process provides the JSON-RPC 2.0 wire protocol types and helpers
// for the external process skill backend. Child processes in any language
// communicate with the agent via JSON-RPC 2.0 over stdin/stdout.
//
// Protocol methods:
//   - initialize: sent after process starts, includes manifest data
//   - tool/execute: invoke a tool registered by the process
//   - hook/handle: dispatch a lifecycle hook event
//   - shutdown: graceful shutdown before killing the process
package process

import (
	"encoding/json"
	"fmt"
)

// JSONRPCRequest represents a JSON-RPC 2.0 request message sent from the agent
// to the child process.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response message received from
// the child process.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error implements the error interface for RPCError.
func (e *RPCError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// NewRequest creates a new JSON-RPC 2.0 request with the given id, method, and
// params. The params value is marshalled to JSON. If params is nil, the params
// field is omitted from the serialized output.
func NewRequest(id int, method string, params any) (*JSONRPCRequest, error) {
	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
	}

	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		req.Params = data
	}

	return req, nil
}

// DecodeResponse unmarshals a JSON-RPC 2.0 response from raw bytes.
func DecodeResponse(data []byte) (*JSONRPCResponse, error) {
	var resp JSONRPCResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &resp, nil
}

// NewInitializeRequest creates a JSON-RPC 2.0 "initialize" request with the
// given manifest data as params.
func NewInitializeRequest(id int, manifest map[string]any) (*JSONRPCRequest, error) {
	return NewRequest(id, "initialize", manifest)
}

// NewToolExecuteRequest creates a JSON-RPC 2.0 "tool/execute" request with the
// tool name and input as params.
func NewToolExecuteRequest(id int, name string, input json.RawMessage) (*JSONRPCRequest, error) {
	params := map[string]any{
		"name":  name,
		"input": input,
	}
	return NewRequest(id, "tool/execute", params)
}

// NewHookHandleRequest creates a JSON-RPC 2.0 "hook/handle" request with the
// hook phase and event data as params.
func NewHookHandleRequest(id int, phase string, data map[string]any) (*JSONRPCRequest, error) {
	params := map[string]any{
		"phase": phase,
		"data":  data,
	}
	return NewRequest(id, "hook/handle", params)
}

// NewShutdownRequest creates a JSON-RPC 2.0 "shutdown" request with no params.
func NewShutdownRequest(id int) (*JSONRPCRequest, error) {
	return NewRequest(id, "shutdown", nil)
}
