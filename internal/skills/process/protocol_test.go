package process

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeRequest(t *testing.T) {
	params := map[string]string{"key": "value"}
	req, err := NewRequest(1, "test/method", params)
	require.NoError(t, err)

	assert.Equal(t, "2.0", req.JSONRPC)
	assert.Equal(t, 1, req.ID)
	assert.Equal(t, "test/method", req.Method)

	// Verify params were marshalled correctly.
	var decoded map[string]string
	err = json.Unmarshal(req.Params, &decoded)
	require.NoError(t, err)
	assert.Equal(t, "value", decoded["key"])

	// Verify the full JSON output is valid JSON-RPC 2.0.
	data, err := json.Marshal(req)
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)
	assert.Contains(t, raw, "jsonrpc")
	assert.Contains(t, raw, "id")
	assert.Contains(t, raw, "method")
	assert.Contains(t, raw, "params")
}

func TestEncodeRequestNilParams(t *testing.T) {
	req, err := NewRequest(5, "no/params", nil)
	require.NoError(t, err)

	assert.Equal(t, "2.0", req.JSONRPC)
	assert.Equal(t, 5, req.ID)
	assert.Equal(t, "no/params", req.Method)
	assert.Nil(t, req.Params)

	// When marshalled, params should be omitted.
	data, err := json.Marshal(req)
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)
	_, hasParams := raw["params"]
	assert.False(t, hasParams, "params should be omitted when nil")
}

func TestDecodeResponse(t *testing.T) {
	respJSON := `{"jsonrpc":"2.0","id":1,"result":{"output":"hello"}}`

	resp, err := DecodeResponse([]byte(respJSON))
	require.NoError(t, err)

	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, 1, resp.ID)
	assert.Nil(t, resp.Error)
	assert.NotNil(t, resp.Result)

	// Verify result content.
	var result map[string]string
	err = json.Unmarshal(resp.Result, &result)
	require.NoError(t, err)
	assert.Equal(t, "hello", result["output"])
}

func TestDecodeResponseError(t *testing.T) {
	respJSON := `{"jsonrpc":"2.0","id":2,"error":{"code":-32601,"message":"method not found"}}`

	resp, err := DecodeResponse([]byte(respJSON))
	require.NoError(t, err)

	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, 2, resp.ID)
	assert.Nil(t, resp.Result)
	require.NotNil(t, resp.Error)
	assert.Equal(t, -32601, resp.Error.Code)
	assert.Equal(t, "method not found", resp.Error.Message)
}

func TestDecodeResponseInvalidJSON(t *testing.T) {
	_, err := DecodeResponse([]byte(`not json`))
	require.Error(t, err)
}

func TestEncodeInitialize(t *testing.T) {
	manifest := map[string]any{
		"name":    "test-skill",
		"version": "1.0.0",
	}

	req, err := NewInitializeRequest(1, manifest)
	require.NoError(t, err)

	assert.Equal(t, "2.0", req.JSONRPC)
	assert.Equal(t, 1, req.ID)
	assert.Equal(t, "initialize", req.Method)

	// Verify params contain the manifest.
	var params map[string]any
	err = json.Unmarshal(req.Params, &params)
	require.NoError(t, err)
	assert.Equal(t, "test-skill", params["name"])
	assert.Equal(t, "1.0.0", params["version"])
}

func TestEncodeToolExecute(t *testing.T) {
	input := json.RawMessage(`{"path":"/tmp/test.txt"}`)

	req, err := NewToolExecuteRequest(2, "read-file", input)
	require.NoError(t, err)

	assert.Equal(t, "2.0", req.JSONRPC)
	assert.Equal(t, 2, req.ID)
	assert.Equal(t, "tool/execute", req.Method)

	// Verify params contain name and input.
	var params map[string]json.RawMessage
	err = json.Unmarshal(req.Params, &params)
	require.NoError(t, err)

	var name string
	err = json.Unmarshal(params["name"], &name)
	require.NoError(t, err)
	assert.Equal(t, "read-file", name)

	var decodedInput map[string]string
	err = json.Unmarshal(params["input"], &decodedInput)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/test.txt", decodedInput["path"])
}

func TestEncodeHookHandle(t *testing.T) {
	data := map[string]any{
		"tool_name": "shell",
		"args":      map[string]any{"command": "ls"},
	}

	req, err := NewHookHandleRequest(3, "OnBeforeToolCall", data)
	require.NoError(t, err)

	assert.Equal(t, "2.0", req.JSONRPC)
	assert.Equal(t, 3, req.ID)
	assert.Equal(t, "hook/handle", req.Method)

	// Verify params contain phase and data.
	var params map[string]json.RawMessage
	err = json.Unmarshal(req.Params, &params)
	require.NoError(t, err)

	var phase string
	err = json.Unmarshal(params["phase"], &phase)
	require.NoError(t, err)
	assert.Equal(t, "OnBeforeToolCall", phase)

	var decodedData map[string]any
	err = json.Unmarshal(params["data"], &decodedData)
	require.NoError(t, err)
	assert.Equal(t, "shell", decodedData["tool_name"])
}

func TestRPCErrorString(t *testing.T) {
	err := &RPCError{Code: -32600, Message: "invalid request"}
	assert.Equal(t, "rpc error -32600: invalid request", err.Error())
}

func TestNewRequestMarshalError(t *testing.T) {
	// A channel cannot be marshalled to JSON.
	unmarshalable := make(chan int)
	_, err := NewRequest(1, "test", unmarshalable)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal params")
}

func TestEncodeShutdown(t *testing.T) {
	req, err := NewShutdownRequest(4)
	require.NoError(t, err)

	assert.Equal(t, "2.0", req.JSONRPC)
	assert.Equal(t, 4, req.ID)
	assert.Equal(t, "shutdown", req.Method)
	assert.Nil(t, req.Params, "shutdown should have no params")

	// Verify that when marshalled, params is omitted.
	data, err := json.Marshal(req)
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)
	_, hasParams := raw["params"]
	assert.False(t, hasParams, "shutdown should omit params field")
}
