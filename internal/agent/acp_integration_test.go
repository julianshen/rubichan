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

// TestAgentACPServerInitialization verifies that the ACP server is properly
// initialized when WithACP option is provided.
func TestAgentACPServerInitialization(t *testing.T) {
	// Create a minimal config
	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test-model"},
		Agent:    config.AgentConfig{MaxTurns: 10},
	}

	// Create a mock provider
	mockProvider := &mockLLMProvider{}

	// Create tool registry
	toolRegistry := tools.NewRegistry()

	// Create agent with ACP enabled
	agentCore := agent.New(
		mockProvider,
		toolRegistry,
		mockApprovalFunc,
		cfg,
		agent.WithACP(),
	)

	if agentCore == nil {
		t.Fatal("agent is nil")
	}

	server := agentCore.ACPServer()
	if server == nil {
		t.Error("ACP server is nil when WithACP was set")
	}
}

// TestAgentACPServerDisabledByDefault verifies that the ACP server is not
// created when WithACP option is not provided.
func TestAgentACPServerDisabledByDefault(t *testing.T) {
	// Create a minimal config
	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test-model"},
		Agent:    config.AgentConfig{MaxTurns: 10},
	}

	// Create a mock provider
	mockProvider := &mockLLMProvider{}

	// Create tool registry
	toolRegistry := tools.NewRegistry()

	// Create agent without WithACP option
	agentCore := agent.New(
		mockProvider,
		toolRegistry,
		mockApprovalFunc,
		cfg,
	)

	if agentCore == nil {
		t.Fatal("agent is nil")
	}

	server := agentCore.ACPServer()
	if server != nil {
		t.Error("ACP server should be nil when WithACP was not set")
	}
}

// TestAgentACPInitializeMethod verifies that the initialize method works correctly.
func TestAgentACPInitializeMethod(t *testing.T) {
	agentCore := createTestAgent(t, true)
	server := agentCore.ACPServer()

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
	agentCore := createTestAgent(t, true)
	server := agentCore.ACPServer()

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
	agentCore := createTestAgent(t, true)
	server := agentCore.ACPServer()

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
	agentCore := createTestAgent(t, true)
	server := agentCore.ACPServer()

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
	agentCore := createTestAgent(t, true)
	server := agentCore.ACPServer()

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

// TestAgentACPCapabilityRegistration verifies that tools are registered as capabilities.
func TestAgentACPCapabilityRegistration(t *testing.T) {
	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test-model"},
		Agent:    config.AgentConfig{MaxTurns: 10},
	}

	mockProvider := &mockLLMProvider{}
	toolRegistry := tools.NewRegistry()

	// Register a test tool
	testTool := &mockTool{
		name:        "test_tool",
		description: "A test tool",
		schema:      json.RawMessage(`{"type":"object"}`),
	}
	if err := toolRegistry.Register(testTool); err != nil {
		t.Fatalf("failed to register test tool: %v", err)
	}

	agentCore := agent.New(
		mockProvider,
		toolRegistry,
		mockApprovalFunc,
		cfg,
		agent.WithACP(),
	)

	server := agentCore.ACPServer()

	// Send initialize request to verify tools are in capabilities
	req := acp.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  json.RawMessage(`{"clientInfo":{"name":"test-client"}}`),
	}

	reqData, _ := json.Marshal(req)
	respData, _ := server.HandleMessage(reqData)

	var resp acp.Response
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatal(err)
	}

	if resp.Result == nil {
		t.Fatal("result is nil")
	}

	var initResult acp.InitializeResult
	if err := json.Unmarshal(*resp.Result, &initResult); err != nil {
		t.Fatalf("failed to unmarshal initialize result: %v", err)
	}

	// Check that tools are in capabilities
	toolCaps, ok := initResult.Capabilities["tool"]
	if !ok {
		t.Error("no tool capabilities in initialize response")
	} else if toolCaps == nil {
		t.Error("tool capabilities is nil")
	}
}

// Helper function to create a test agent with ACP enabled
func createTestAgent(t *testing.T, enableACP bool) *agent.Agent {
	cfg := &config.Config{
		Provider: config.ProviderConfig{Model: "test-model"},
		Agent:    config.AgentConfig{MaxTurns: 10},
	}

	mockProvider := &mockLLMProvider{}
	toolRegistry := tools.NewRegistry()

	opts := []agent.AgentOption{}
	if enableACP {
		opts = append(opts, agent.WithACP())
	}

	return agent.New(
		mockProvider,
		toolRegistry,
		mockApprovalFunc,
		cfg,
		opts...,
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
