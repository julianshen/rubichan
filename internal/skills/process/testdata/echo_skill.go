// echo_skill is a test helper binary that implements the external process skill
// protocol. It reads JSON-RPC 2.0 requests from stdin (one per line) and writes
// JSON-RPC 2.0 responses to stdout.
//
// Supported methods:
//   - initialize: responds with tool and hook declarations
//   - tool/execute: echoes back the input with tool name
//   - hook/handle: responds with empty result
//   - shutdown: exits cleanly
//   - slow/method: sleeps for 5 seconds (used to test timeouts)
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// request mirrors the JSON-RPC 2.0 request structure.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// response mirrors the JSON-RPC 2.0 response structure.
type response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	// Increase buffer size for large messages.
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()

		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			writeError(0, -32700, fmt.Sprintf("parse error: %v", err))
			continue
		}

		switch req.Method {
		case "initialize":
			handleInitialize(req)
		case "tool/execute":
			handleToolExecute(req)
		case "hook/handle":
			handleHookHandle(req)
		case "shutdown":
			handleShutdown(req)
		case "slow/method":
			handleSlow(req)
		default:
			writeError(req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
		}
	}
}

func handleInitialize(req request) {
	result := map[string]interface{}{
		"tools": []map[string]interface{}{
			{
				"name":        "echo",
				"description": "Echoes back the input",
				"input_schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"message": map[string]interface{}{
							"type":        "string",
							"description": "The message to echo",
						},
					},
				},
			},
		},
		"hooks": []string{"OnBeforeToolCall"},
	}
	writeResult(req.ID, result)
}

func handleToolExecute(req request) {
	var params map[string]json.RawMessage
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeError(req.ID, -32602, fmt.Sprintf("invalid params: %v", err))
		return
	}

	var name string
	if err := json.Unmarshal(params["name"], &name); err != nil {
		writeError(req.ID, -32602, fmt.Sprintf("invalid tool name: %v", err))
		return
	}

	result := map[string]interface{}{
		"content":  fmt.Sprintf("echo: tool=%s input=%s", name, string(params["input"])),
		"is_error": false,
	}
	writeResult(req.ID, result)
}

func handleHookHandle(req request) {
	result := map[string]interface{}{
		"modified": map[string]interface{}{},
		"cancel":   false,
	}
	writeResult(req.ID, result)
}

func handleShutdown(req request) {
	writeResult(req.ID, map[string]interface{}{"status": "ok"})
	os.Exit(0)
}

func handleSlow(req request) {
	time.Sleep(5 * time.Second)
	writeResult(req.ID, map[string]interface{}{"status": "slow"})
}

func writeResult(id int, result interface{}) {
	resp := response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	data, _ := json.Marshal(resp)
	fmt.Fprintln(os.Stdout, string(data))
}

func writeError(id int, code int, message string) {
	resp := response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: message},
	}
	data, _ := json.Marshal(resp)
	fmt.Fprintln(os.Stdout, string(data))
}
