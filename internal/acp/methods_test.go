package acp

import (
	"encoding/json"
	"testing"
)

func TestListToolsRequest(t *testing.T) {
	msg := Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  MethodListTools,
		Params:  nil,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	var unmarshaled Request
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatal(err)
	}

	if unmarshaled.Method != MethodListTools {
		t.Errorf("got %q, want %q", unmarshaled.Method, MethodListTools)
	}
}

func TestCallToolRequest(t *testing.T) {
	params := json.RawMessage(`{
		"name": "file_read",
		"arguments": {"path": "/tmp/test.txt"}
	}`)

	msg := Request{
		JSONRPC: "2.0",
		ID:      2,
		Method:  MethodCallTool,
		Params:  params,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	var unmarshaled Request
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatal(err)
	}

	if unmarshaled.Method != MethodCallTool {
		t.Errorf("got %q, want %q", unmarshaled.Method, MethodCallTool)
	}
}

func TestErrorCodeConstants(t *testing.T) {
	if ErrorCodeMethodNotFound != -32601 {
		t.Errorf("got %d, want -32601", ErrorCodeMethodNotFound)
	}
	if ErrorCodeToolNotFound != -32100 {
		t.Errorf("got %d, want -32100", ErrorCodeToolNotFound)
	}
}

func TestListResourcesRequest(t *testing.T) {
	msg := Request{
		JSONRPC: "2.0",
		ID:      3,
		Method:  MethodListResources,
		Params:  nil,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	var unmarshaled Request
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatal(err)
	}

	if unmarshaled.Method != MethodListResources {
		t.Errorf("got %q, want %q", unmarshaled.Method, MethodListResources)
	}
}

func TestCallSkillRequest(t *testing.T) {
	params := json.RawMessage(`{
		"name": "my_skill",
		"arguments": {}
	}`)

	msg := Request{
		JSONRPC: "2.0",
		ID:      4,
		Method:  MethodCallSkill,
		Params:  params,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	var unmarshaled Request
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatal(err)
	}

	if unmarshaled.Method != MethodCallSkill {
		t.Errorf("got %q, want %q", unmarshaled.Method, MethodCallSkill)
	}
}

func TestAllErrorCodesAreDefined(t *testing.T) {
	tests := []struct {
		name     string
		code     int
		expected int
	}{
		{"ParseError", ErrorCodeParseError, -32700},
		{"InvalidRequest", ErrorCodeInvalidRequest, -32600},
		{"MethodNotFound", ErrorCodeMethodNotFound, -32601},
		{"InvalidParams", ErrorCodeInvalidParams, -32602},
		{"InternalError", ErrorCodeInternalError, -32603},
		{"ServerError", ErrorCodeServerError, -32000},
		{"ToolNotFound", ErrorCodeToolNotFound, -32100},
		{"SkillNotFound", ErrorCodeSkillNotFound, -32101},
		{"PermissionDenied", ErrorCodePermissionDenied, -32102},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.code != tt.expected {
				t.Errorf("got %d, want %d", tt.code, tt.expected)
			}
		})
	}
}
