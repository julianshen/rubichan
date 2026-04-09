package acp_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/acp"
)

func TestServerInitialize(t *testing.T) {
	registry := acp.NewCapabilityRegistry()

	// Register a dummy initialize handler
	registry.RegisterMethod("initialize", func(params json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"status":"ready"}`), nil
	})

	server := acp.NewServer(registry)

	// Create a request
	req := acp.Request{
		JSONRPC: "2.0",
		ID:      int64(1),
		Method:  "initialize",
		Params:  json.RawMessage(`{"clientInfo":{"name":"test"}}`),
	}

	reqData, _ := json.Marshal(req)

	// Process the request
	respData, err := server.HandleMessage(reqData)
	if err != nil {
		t.Fatal(err)
	}

	// Parse response
	var resp acp.Response
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatal(err)
	}

	// ID becomes float64 when unmarshaled from JSON
	if idFloat, ok := resp.ID.(float64); !ok || idFloat != 1.0 {
		t.Errorf("got ID %v, want 1.0", resp.ID)
	}
	if resp.Error != nil {
		t.Errorf("got error %v, want nil", resp.Error)
	}
	if resp.Result == nil {
		t.Error("result is nil")
	}
}

func TestServerMethodNotFound(t *testing.T) {
	registry := acp.NewCapabilityRegistry()
	server := acp.NewServer(registry)

	req := acp.Request{
		JSONRPC: "2.0",
		ID:      int64(1),
		Method:  "nonexistent",
	}

	reqData, _ := json.Marshal(req)
	respData, _ := server.HandleMessage(reqData)

	var resp acp.Response
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatal(err)
	}

	if resp.Error == nil {
		t.Error("expected error for nonexistent method")
	}
	if resp.Error.Code != acp.ErrorCodeMethodNotFound {
		t.Errorf("got error code %d, want %d", resp.Error.Code, acp.ErrorCodeMethodNotFound)
	}
}

func TestStdioTransport(t *testing.T) {
	// Create pipes for stdin/stdout
	stdin := bytes.NewReader(nil)
	stdout := &bytes.Buffer{}

	registry := acp.NewCapabilityRegistry()
	registry.RegisterMethod("test/ping", func(params json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"pong":true}`), nil
	})

	server := acp.NewServer(registry)
	transport := acp.NewStdioTransport(stdin, stdout, server)

	// Verify transport is initialized
	if transport == nil {
		t.Error("transport is nil")
	}
}

func TestServerShutdown(t *testing.T) {
	registry := acp.NewCapabilityRegistry()
	server := acp.NewServer(registry)

	req := acp.Request{
		JSONRPC: "2.0",
		ID:      int64(1),
		Method:  "shutdown",
	}

	reqData, _ := json.Marshal(req)
	respData, _ := server.HandleMessage(reqData)

	var resp acp.Response
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatal(err)
	}

	if resp.Error != nil {
		t.Errorf("got error %v, want nil", resp.Error)
	}
	if resp.Result == nil {
		t.Error("result is nil")
	}
}

func TestServerCustomMethod(t *testing.T) {
	registry := acp.NewCapabilityRegistry()
	registry.RegisterMethod("test/echo", func(params json.RawMessage) (json.RawMessage, error) {
		return params, nil
	})

	server := acp.NewServer(registry)

	req := acp.Request{
		JSONRPC: "2.0",
		ID:      int64(2),
		Method:  "test/echo",
		Params:  json.RawMessage(`{"message":"hello"}`),
	}

	reqData, _ := json.Marshal(req)
	respData, _ := server.HandleMessage(reqData)

	var resp acp.Response
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatal(err)
	}

	if resp.Error != nil {
		t.Errorf("got error, want nil")
	}
	if string(*resp.Result) != `{"message":"hello"}` {
		t.Errorf("got %s, want echo of input", string(*resp.Result))
	}
}

func TestServerParseError(t *testing.T) {
	server := acp.NewServer(acp.NewCapabilityRegistry())
	respData, err := server.HandleMessage([]byte(`{invalid json}`))
	if err != nil {
		t.Fatalf("HandleMessage should not return error, got %v", err)
	}

	var resp acp.Response
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatal(err)
	}

	if resp.Error == nil {
		t.Error("expected error response for invalid JSON")
	}
	if resp.Error.Code != acp.ErrorCodeParseError {
		t.Errorf("got error code %d, want %d", resp.Error.Code, acp.ErrorCodeParseError)
	}
}

func TestServerInvalidParams(t *testing.T) {
	registry := acp.NewCapabilityRegistry()
	server := acp.NewServer(registry)

	// Initialize method with params that is valid JSON but not valid InitializeParams
	// Pass a number instead of an object
	req := acp.Request{
		JSONRPC: "2.0",
		ID:      int64(1),
		Method:  "initialize",
		Params:  json.RawMessage(`123`),
	}

	reqData, _ := json.Marshal(req)
	respData, _ := server.HandleMessage(reqData)

	var resp acp.Response
	json.Unmarshal(respData, &resp)

	if resp.Error == nil {
		t.Error("expected error for invalid params")
	}
	if resp.Error.Code != acp.ErrorCodeInvalidParams {
		t.Errorf("got code %d, want %d", resp.Error.Code, acp.ErrorCodeInvalidParams)
	}
}

func TestStdioTransportHandlesMessages(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"test/ping","params":null}` + "\n"
	stdin := bytes.NewBufferString(input)
	stdout := &bytes.Buffer{}

	registry := acp.NewCapabilityRegistry()
	registry.RegisterMethod("test/ping", func(params json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"pong":true}`), nil
	})

	server := acp.NewServer(registry)
	transport := acp.NewStdioTransport(stdin, stdout, server)

	err := transport.Start()
	if err != nil && err.Error() != "EOF" {
		t.Fatalf("Start failed unexpectedly: %v", err)
	}

	// Verify response was written
	respOutput := stdout.String()
	if len(respOutput) == 0 {
		t.Error("expected response output, got nothing")
	}

	// Verify response contains expected content
	var resp acp.Response
	if err := json.Unmarshal([]byte(respOutput), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	// ID is unmarshaled as float64 from JSON
	idFloat, ok := resp.ID.(float64)
	if !ok || idFloat != 1.0 {
		t.Errorf("got ID %v, want 1.0", resp.ID)
	}
	if resp.Error != nil {
		t.Errorf("got error, want success response")
	}
}
