package agent_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/acp"
	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// TestAgentACPInitializeMethod verifies that the initialize method works correctly.
func TestAgentACPInitializeMethod(t *testing.T) {
	server := agent.NewACPServer(createTestAgent(t))

	// Send initialize request
	req := acp.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  json.RawMessage(`{"clientInfo":{"name":"test-client"}}`),
	}

	reqData, _ := json.Marshal(req)
	respData, err := server.HandleMessage(reqData)
	if err != nil {
		t.Fatalf("handle message failed: %v", err)
	}

	var resp acp.Response
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatal(err)
	}

	if resp.Error != nil {
		t.Errorf("got error %v, expected success", resp.Error)
	}
	if resp.Result == nil {
		t.Error("result is nil")
	}
}

// TestAgentACPPromptMethod verifies that the agent/prompt method handles requests correctly.
func TestAgentACPPromptMethod(t *testing.T) {
	server := agent.NewACPServer(createTestAgent(t))

	// Send prompt request
	req := acp.Request{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "agent/prompt",
		Params:  json.RawMessage(`{"prompt":"test prompt","maxTurns":1}`),
	}

	reqData, _ := json.Marshal(req)
	respData, _ := server.HandleMessage(reqData)

	var resp acp.Response
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Result == nil && resp.Error == nil {
		t.Error("expected result or error")
	}
	if resp.Result != nil {
		var result map[string]interface{}
		if err := json.Unmarshal(*resp.Result, &result); err != nil {
			t.Fatalf("failed to unmarshal result: %v", err)
		}
		if status, ok := result["status"].(string); !ok || status != "complete" {
			t.Errorf("expected status='complete', got %v", result["status"])
		}
	}
}

// TestAgentACPToolExecution verifies that the tool/execute method works correctly.
func TestAgentACPToolExecution(t *testing.T) {
	server := agent.NewACPServer(createTestAgent(t))

	// Send tool execution request
	req := acp.Request{
		JSONRPC: "2.0",
		ID:      3,
		Method:  "tool/execute",
		Params:  json.RawMessage(`{"tool":"unknown_tool","input":{"path":"main.go"}}`),
	}

	reqData, _ := json.Marshal(req)
	respData, _ := server.HandleMessage(reqData)

	var resp acp.Response
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Should return an error because the tool doesn't exist
	if resp.Error == nil {
		t.Error("expected error for non-existent tool")
	}
}

// TestAgentACPSkillMethods verifies that skill methods are properly registered.
func TestAgentACPSkillMethods(t *testing.T) {
	server := agent.NewACPServer(createTestAgent(t))

	// Test skill/invoke
	req := acp.Request{
		JSONRPC: "2.0",
		ID:      4,
		Method:  "skill/invoke",
		Params:  json.RawMessage(`{"skillName":"test","action":"transform","input":{}}`),
	}

	reqData, _ := json.Marshal(req)
	respData, _ := server.HandleMessage(reqData)

	var resp acp.Response
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Result == nil && resp.Error == nil {
		t.Error("expected result or error")
	}
}

// TestAgentACPSecurityMethods verifies that security methods are properly registered.
func TestAgentACPSecurityMethods(t *testing.T) {
	server := agent.NewACPServer(createTestAgent(t))

	// Test security/scan
	req := acp.Request{
		JSONRPC: "2.0",
		ID:      5,
		Method:  "security/scan",
		Params:  json.RawMessage(`{"scope":"project","target":"./","interactive":false}`),
	}

	reqData, _ := json.Marshal(req)
	respData, _ := server.HandleMessage(reqData)

	var resp acp.Response
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Result == nil && resp.Error == nil {
		t.Error("expected result or error")
	}
}

// TestNewACPServerComposesOverPlainAgent pins the Transport seam: the ACP
// server is composed over an agent at the composition root, with no core
// flag or field involved — a plain agent (no WithACP) plus NewACPServer
// yields a fully capable server (initialize succeeds, registered tools
// appear as capabilities, agent methods are routed).
func TestNewACPServerComposesOverPlainAgent(t *testing.T) {
	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test-model"},
		Agent:    config.AgentConfig{MaxTurns: 10},
	}

	toolRegistry := tools.NewRegistry()
	testTool := &mockTool{
		name:        "test_tool",
		description: "A test tool",
		schema:      json.RawMessage(`{"type":"object"}`),
	}
	if err := toolRegistry.Register(testTool); err != nil {
		t.Fatalf("failed to register test tool: %v", err)
	}

	agentCore := agent.New(&mockLLMProvider{}, toolRegistry, mockApprovalFunc, cfg)

	server := agent.NewACPServer(agentCore)
	if server == nil {
		t.Fatal("NewACPServer returned nil")
	}

	// initialize must succeed and expose the registered tool capability.
	req := acp.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  json.RawMessage(`{"clientInfo":{"name":"test-client"}}`),
	}
	reqData, _ := json.Marshal(req)
	respData, err := server.HandleMessage(reqData)
	if err != nil {
		t.Fatalf("handle message failed: %v", err)
	}

	var resp acp.Response
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil {
		t.Fatalf("initialize returned error: %v", resp.Error)
	}
	var initResult acp.InitializeResult
	if err := json.Unmarshal(*resp.Result, &initResult); err != nil {
		t.Fatalf("failed to unmarshal initialize result: %v", err)
	}
	if _, ok := initResult.Capabilities["tool"]; !ok {
		t.Error("no tool capabilities in initialize response")
	}

	// A registered agent method must be routed (result or a handler error,
	// not a method-not-found transport error).
	req = acp.Request{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "skill/invoke",
		Params:  json.RawMessage(`{"skillName":"test","action":"transform","input":{}}`),
	}
	reqData, _ = json.Marshal(req)
	respData, _ = server.HandleMessage(reqData)
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.Result == nil && resp.Error == nil {
		t.Error("expected result or error from routed skill method")
	}
	if resp.Error != nil && resp.Error.Code == acp.ErrorCodeMethodNotFound {
		t.Error("skill/invoke not registered on composed server")
	}
}

// Helper function to create a plain test agent; compose an ACP server
// over it with agent.NewACPServer.
func createTestAgent(t *testing.T) *agent.Agent {
	t.Helper()
	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test-model"},
		Agent:    config.AgentConfig{MaxTurns: 10},
	}

	return agent.New(
		&mockLLMProvider{},
		tools.NewRegistry(),
		mockApprovalFunc,
		cfg,
	)
}

// Mock implementations for testing

var mockApprovalFunc agentsdk.ApprovalFunc = func(ctx context.Context, tool string, input json.RawMessage) (bool, error) {
	return true, nil
}

type mockLLMProvider struct{}

func (m *mockLLMProvider) Stream(
	ctx context.Context,
	req agentsdk.CompletionRequest,
) (<-chan agentsdk.StreamEvent, error) {
	ch := make(chan agentsdk.StreamEvent)
	close(ch)
	return ch, nil
}

type mockTool struct {
	name        string
	description string
	schema      json.RawMessage
}

func (m *mockTool) Name() string {
	return m.name
}

func (m *mockTool) Description() string {
	return m.description
}

func (m *mockTool) InputSchema() json.RawMessage {
	return m.schema
}

func (m *mockTool) Execute(ctx context.Context, input json.RawMessage) (agentsdk.ToolResult, error) {
	return agentsdk.ToolResult{
		Content: "ok",
	}, nil
}
