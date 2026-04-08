package acp_test

import (
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/acp"
)

func TestSkillInvokeRequest(t *testing.T) {
	req := acp.SkillInvokeRequest{
		SkillName: "my_skill",
		Action:    "transform",
		Input:     json.RawMessage(`{"code":"print('hello')"}`),
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}

	var decoded acp.SkillInvokeRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.SkillName != "my_skill" {
		t.Errorf("got %q, want my_skill", decoded.SkillName)
	}
	if decoded.Action != "transform" {
		t.Errorf("got %q, want transform", decoded.Action)
	}
}

func TestSkillInvokeResponse(t *testing.T) {
	resp := acp.SkillInvokeResponse{
		SkillName: "my_skill",
		Action:    "transform",
		Output:    json.RawMessage(`{"result":"success"}`),
		Status:    "success",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}

	var decoded acp.SkillInvokeResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Status != "success" {
		t.Errorf("got %q, want success", decoded.Status)
	}
}

func TestSkillListRequest(t *testing.T) {
	req := acp.SkillListRequest{
		Filter: "transform",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}

	var decoded acp.SkillListRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Filter != "transform" {
		t.Errorf("got %q, want transform", decoded.Filter)
	}
}

func TestSkillListResponse(t *testing.T) {
	resp := acp.SkillListResponse{
		Skills: []acp.SkillSummary{
			{
				Name:        "my_skill",
				Version:     "1.0",
				Description: "Test skill",
				Type:        "starlark",
				Actions:     []string{"transform"},
				Permissions: []string{"file:read"},
			},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}

	var decoded acp.SkillListResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if len(decoded.Skills) != 1 {
		t.Errorf("got %d skills, want 1", len(decoded.Skills))
	}
	if decoded.Skills[0].Name != "my_skill" {
		t.Errorf("got %q, want my_skill", decoded.Skills[0].Name)
	}
}

func TestSkillManifestRequest(t *testing.T) {
	req := acp.SkillManifestRequest{
		SkillName: "my_skill",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}

	var decoded acp.SkillManifestRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.SkillName != "my_skill" {
		t.Errorf("got %q, want my_skill", decoded.SkillName)
	}
}

func TestSkillManifestResponse(t *testing.T) {
	resp := acp.SkillManifestResponse{
		Name:        "my_skill",
		Version:     "1.0",
		Manifest:    json.RawMessage(`{"backend":"starlark"}`),
		Permissions: []string{"file:read"},
		Backend:     "starlark",
		Status:      "loaded",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}

	var decoded acp.SkillManifestResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Status != "loaded" {
		t.Errorf("got %q, want loaded", decoded.Status)
	}
}

func TestSkillApprovalRequest(t *testing.T) {
	req := acp.SkillApprovalRequest{
		SkillName:   "dangerous_skill",
		Action:      "delete",
		Permissions: []string{"file:delete"},
		Reason:      "Requires user approval for destructive operation",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}

	var decoded acp.SkillApprovalRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.SkillName != "dangerous_skill" {
		t.Errorf("got %q, want dangerous_skill", decoded.SkillName)
	}
}

func TestSkillApprovalResponse(t *testing.T) {
	resp := acp.SkillApprovalResponse{
		Approved: true,
		Reason:   "User approved",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}

	var decoded acp.SkillApprovalResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if !decoded.Approved {
		t.Error("expected approved to be true")
	}
}

func TestRegisterSkillMethods(t *testing.T) {
	registry := acp.NewCapabilityRegistry()

	// Mock skill invoker
	invoker := &MockSkillInvoker{
		invoked: false,
	}

	// Register methods
	acp.RegisterSkillMethods(registry, invoker)

	// Verify methods are registered
	methods := registry.GetMethods()
	if len(methods) != 3 {
		t.Errorf("got %d methods, want 3", len(methods))
	}

	// Verify skill/invoke is callable
	params := json.RawMessage(`{"skillName":"test","action":"transform","input":{}}`)
	result, err := registry.Call("skill/invoke", params)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	var resp acp.SkillInvokeResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if !invoker.invoked {
		t.Error("expected invoker to be called")
	}
}

// MockSkillInvoker for testing
type MockSkillInvoker struct {
	invoked bool
}

func (m *MockSkillInvoker) Invoke(req acp.SkillInvokeRequest) (acp.SkillInvokeResponse, error) {
	m.invoked = true
	return acp.SkillInvokeResponse{
		SkillName: req.SkillName,
		Action:    req.Action,
		Output:    json.RawMessage(`{"result":"ok"}`),
		Status:    "success",
	}, nil
}

func (m *MockSkillInvoker) List(req acp.SkillListRequest) acp.SkillListResponse {
	return acp.SkillListResponse{
		Skills: []acp.SkillSummary{
			{
				Name:    "test_skill",
				Version: "1.0",
				Type:    "starlark",
			},
		},
	}
}

func (m *MockSkillInvoker) Manifest(req acp.SkillManifestRequest) (acp.SkillManifestResponse, error) {
	return acp.SkillManifestResponse{
		Name:    req.SkillName,
		Version: "1.0",
		Status:  "loaded",
	}, nil
}
