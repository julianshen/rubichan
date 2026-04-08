package acp

import (
	"encoding/json"
)

// Server is the ACP JSON-RPC 2.0 server.
type Server struct {
	registry *CapabilityRegistry
}

// NewServer creates a new ACP server.
func NewServer(registry *CapabilityRegistry) *Server {
	return &Server{
		registry: registry,
	}
}

// HandleMessage processes a single JSON-RPC message and returns a response.
func (s *Server) HandleMessage(data []byte) ([]byte, error) {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		// Parse error → return error response
		return s.errorResponse(0, ErrorCodeParseError, "Parse error", nil), nil
	}

	// Handle built-in methods
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "shutdown":
		return s.handleShutdown(req)
	default:
		// Delegate to registry
		return s.handleCustomMethod(req)
	}
}

func (s *Server) handleInitialize(req Request) ([]byte, error) {
	var initParams InitializeParams
	if err := json.Unmarshal(req.Params, &initParams); err != nil {
		return s.errorResponse(req.ID, ErrorCodeInvalidParams, "Invalid params", nil), nil
	}

	caps, err := s.registry.GetCapabilities()
	if err != nil {
		return s.errorResponse(req.ID, ErrorCodeInternalError, "Internal error", nil), nil
	}

	initResult := InitializeResult{
		ServerInfo: ServerInfo{
			Name:    "rubichan",
			Version: "1.0.0",
		},
		Capabilities: make(map[string]interface{}),
	}

	// Add capabilities to the response
	for _, cap := range caps {
		if initResult.Capabilities[cap.Type] == nil {
			initResult.Capabilities[cap.Type] = []interface{}{}
		}
		capList := initResult.Capabilities[cap.Type].([]interface{})
		capList = append(capList, cap)
		initResult.Capabilities[cap.Type] = capList
	}

	result, _ := json.Marshal(initResult)
	return s.successResponse(req.ID, json.RawMessage(result)), nil
}

func (s *Server) handleShutdown(req Request) ([]byte, error) {
	return s.successResponse(req.ID, json.RawMessage(`{"status":"shutdown"}`)), nil
}

func (s *Server) handleCustomMethod(req Request) ([]byte, error) {
	result, err := s.registry.Call(req.Method, req.Params)
	if err != nil {
		return s.errorResponse(req.ID, ErrorCodeMethodNotFound, "Method not found", nil), nil
	}
	return s.successResponse(req.ID, result), nil
}

func (s *Server) successResponse(id interface{}, result json.RawMessage) []byte {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  &result,
	}
	data, _ := json.Marshal(resp)
	return data
}

func (s *Server) errorResponse(id interface{}, code int, message string, data interface{}) []byte {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
		},
	}
	respData, _ := json.Marshal(resp)
	return respData
}
