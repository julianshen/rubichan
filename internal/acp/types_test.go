package acp

import (
	"encoding/json"
	"testing"
)

func TestACPRequest(t *testing.T) {
	req := Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  json.RawMessage(`{"clientInfo":{"name":"rubichan-tui"}}`),
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}

	var decoded Request
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Method != "initialize" {
		t.Errorf("got %q, want %q", decoded.Method, "initialize")
	}
}

func TestACPResponse(t *testing.T) {
	result := json.RawMessage(`{"status":"ready"}`)
	resp := Response{
		JSONRPC: "2.0",
		ID:      1,
		Result:  &result,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}

	var decoded Response
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Result == nil {
		t.Error("result is nil")
	}
}

func TestACPError(t *testing.T) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      1,
		Error: &RPCError{
			Code:    -32601,
			Message: "Method not found",
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}

	var decoded Response
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Error == nil {
		t.Error("error is nil")
	}
	if decoded.Error.Code != -32601 {
		t.Errorf("got code %d, want -32601", decoded.Error.Code)
	}
}

func TestToolCapability(t *testing.T) {
	toolCap := ToolCapability{
		Tool: Tool{
			Name:        "file.read",
			Description: "Read file contents",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		},
	}

	data, err := json.Marshal(toolCap)
	if err != nil {
		t.Fatal(err)
	}

	var decoded ToolCapability
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Tool.Name != "file.read" {
		t.Errorf("got %q, want %q", decoded.Tool.Name, "file.read")
	}
}

func TestSkillCapability(t *testing.T) {
	skillCap := SkillCapability{
		Skill: Skill{
			Name:        "my_skill",
			Manifest:    json.RawMessage(`{"version":"1.0","backend":"starlark"}`),
			Permissions: []string{"file:read", "shell:exec"},
		},
	}

	data, err := json.Marshal(skillCap)
	if err != nil {
		t.Fatal(err)
	}

	var decoded SkillCapability
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if len(decoded.Skill.Permissions) != 2 {
		t.Errorf("got %d permissions, want 2", len(decoded.Skill.Permissions))
	}
}

func TestSecurityVerdict(t *testing.T) {
	verdict := SecurityVerdict{
		ID:         "finding-1",
		Status:     "flagged",
		Severity:   "high",
		Message:    "Hardcoded secret detected",
		Confidence: 0.95,
	}

	data, err := json.Marshal(verdict)
	if err != nil {
		t.Fatal(err)
	}

	var decoded SecurityVerdict
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Severity != "high" {
		t.Errorf("got %q, want %q", decoded.Severity, "high")
	}
}

func TestCapabilityRegistry(t *testing.T) {
	registry := NewCapabilityRegistry()

	tool := Tool{
		Name:        "file.read",
		Description: "Read a file",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}

	registry.RegisterTool(tool)

	caps, err := registry.GetCapabilities()
	if err != nil {
		t.Fatal(err)
	}

	if len(caps) == 0 {
		t.Error("expected at least 1 capability")
	}
}

func TestCapabilityRegistryMethods(t *testing.T) {
	registry := NewCapabilityRegistry()

	callCount := 0
	registry.RegisterMethod("test/ping", func(params json.RawMessage) (json.RawMessage, error) {
		callCount++
		return json.RawMessage(`{"pong":true}`), nil
	})

	methods := registry.GetMethods()
	if len(methods) == 0 {
		t.Error("expected methods list")
	}
}
