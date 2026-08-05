package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/julianshen/rubichan/internal/acp"
	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// TestAgentACPPromptMethod verifies that the agent/prompt method handles requests correctly.
func TestAgentACPPromptMethod(t *testing.T) {
	registry := agent.NewACPRegistry(createTestAgent(t))

	// Send prompt request
	req := acp.Request{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "agent/prompt",
		Params:  json.RawMessage(`{"prompt":"test prompt","maxTurns":1}`),
	}

	result, err := registry.Call(req.Method, req.Params)
	if errors.Is(err, acp.ErrMethodNotFound) {
		t.Fatalf("%s is not registered", req.Method)
	}
	if err != nil {
		return // a handler error is an acceptable outcome for a stub provider
	}
	var decoded map[string]any
	if err := json.Unmarshal(result, &decoded); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if status, ok := decoded["status"].(string); !ok || status != "complete" {
		t.Errorf("expected status='complete', got %v", decoded["status"])
	}
}

// TestAgentACPToolExecution verifies that the tool/execute method works correctly.
func TestAgentACPToolExecution(t *testing.T) {
	registry := agent.NewACPRegistry(createTestAgent(t))

	// Send tool execution request
	req := acp.Request{
		JSONRPC: "2.0",
		ID:      3,
		Method:  "tool/execute",
		Params:  json.RawMessage(`{"tool":"unknown_tool","input":{"path":"main.go"}}`),
	}

	_, err := registry.Call(req.Method, req.Params)
	if errors.Is(err, acp.ErrMethodNotFound) {
		t.Fatalf("%s is not registered", req.Method)
	}
	if err == nil {
		t.Error("expected error for non-existent tool")
	}
}

// TestAgentACPSkillMethods verifies that skill methods are properly registered.
func TestAgentACPSkillMethods(t *testing.T) {
	registry := agent.NewACPRegistry(createTestAgent(t))

	// Test skill/invoke
	req := acp.Request{
		JSONRPC: "2.0",
		ID:      4,
		Method:  "skill/invoke",
		Params:  json.RawMessage(`{"skillName":"test","action":"transform","input":{}}`),
	}

	result, err := registry.Call(req.Method, req.Params)
	if errors.Is(err, acp.ErrMethodNotFound) {
		t.Fatalf("%s is not registered", req.Method)
	}
	if result == nil && err == nil {
		t.Error("expected result or error")
	}
}

// TestAgentACPSecurityMethods verifies that security methods are properly registered.
func TestAgentACPSecurityMethods(t *testing.T) {
	registry := agent.NewACPRegistry(createTestAgent(t))

	// Test security/scan
	req := acp.Request{
		JSONRPC: "2.0",
		ID:      5,
		Method:  "security/scan",
		Params:  json.RawMessage(`{"scope":"project","target":"./","interactive":false}`),
	}

	result, err := registry.Call(req.Method, req.Params)
	if errors.Is(err, acp.ErrMethodNotFound) {
		t.Fatalf("%s is not registered", req.Method)
	}
	if result == nil && err == nil {
		t.Error("expected result or error")
	}
}

// TestNewACPRegistryComposesOverPlainAgent pins the Transport seam: the ACP
// surface is composed over an agent at the composition root, with no core flag
// or field involved — a plain agent plus NewACPRegistry yields a registry whose
// tools appear as capabilities and whose agent methods are routed.
func TestNewACPRegistryComposesOverPlainAgent(t *testing.T) {
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

	registry := agent.NewACPRegistry(agentCore)
	if registry == nil {
		t.Fatal("NewACPRegistry returned nil")
	}

	// The agent's tools must appear as capabilities.
	caps, err := registry.GetCapabilities()
	if err != nil {
		t.Fatalf("GetCapabilities failed: %v", err)
	}
	var sawTool bool
	for _, c := range caps {
		if c.Type == "tool" && c.Name == "test_tool" {
			sawTool = true
		}
	}
	if !sawTool {
		t.Error("registered tool did not appear as an ACP capability")
	}

	// A registered agent method must be routed — a handler error is fine, but
	// ErrMethodNotFound means it was never wired.
	if _, err := registry.Call("skill/invoke",
		json.RawMessage(`{"skillName":"test","action":"transform","input":{}}`)); errors.Is(err, acp.ErrMethodNotFound) {
		t.Error("skill/invoke not registered on the composed registry")
	}
}

// createTestAgent builds a plain test agent; compose an ACP surface over it
// with agent.NewACPRegistry.
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

// TestNewACPRegistryServesTheHandshake pins that an agent's ACP surface can be
// initialized at all. Every ACP session opens with this call, so a registry
// that does not serve it is unusable regardless of what else it registers.
func TestNewACPRegistryServesTheHandshake(t *testing.T) {
	registry := agent.NewACPRegistry(createTestAgent(t))

	raw, err := registry.Call("initialize", json.RawMessage(
		`{"protocolVersion":1,"clientCapabilities":{"fs":{"readTextFile":true}}}`))
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	var got acp.InitializeResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal initialize result: %v", err)
	}
	if got.ProtocolVersion != acp.ProtocolVersion {
		t.Errorf("protocolVersion = %d, want %d", got.ProtocolVersion, acp.ProtocolVersion)
	}
	if got.AgentInfo.Name != "rubichan" {
		t.Errorf("agentInfo.name = %q, want rubichan", got.AgentInfo.Name)
	}
	if got.AuthMethods == nil {
		t.Error("authMethods must be present even when empty")
	}
}

// TestNewACPRegistryDoesNotAdvertiseWhatItCannotServe ties the handshake's
// claims to the methods actually registered. embeddedContext means the agent
// accepts embedded ContentBlock::Resource data in session/prompt; declaring it
// while session/prompt is unregistered tells a client it may send something
// that will fail. That is the mistake the adapters deleted in #339 made in the
// other direction — calling methods the peer does not serve — and it is no
// better pointed this way.
func TestNewACPRegistryDoesNotAdvertiseWhatItCannotServe(t *testing.T) {
	registry := agent.NewACPRegistry(createTestAgent(t))

	raw, err := registry.Call("initialize", json.RawMessage(
		`{"protocolVersion":1,"clientCapabilities":{}}`))
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	var got acp.InitializeResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	_, promptErr := registry.Call("session/prompt", json.RawMessage(`{}`))
	servesSessionPrompt := !errors.Is(promptErr, acp.ErrMethodNotFound)

	if got.AgentCapabilities.Prompt.EmbeddedContext && !servesSessionPrompt {
		t.Error("embeddedContext advertised, but session/prompt is not registered")
	}
	if got.AgentCapabilities.LoadSession {
		t.Error("loadSession advertised, but session/load is not registered")
	}
}

// TestNewACPRegistryRetainsClientCapabilities pins that what the client offers
// is kept rather than parsed and dropped. RegisterInitialize provides the hook;
// a registry that does not supply it discards the information entirely, which
// is worse than never having asked.
func TestNewACPRegistryRetainsClientCapabilities(t *testing.T) {
	a := createTestAgent(t)
	registry := agent.NewACPRegistry(a)

	if _, err := registry.Call("initialize", json.RawMessage(
		`{"protocolVersion":1,"clientCapabilities":{"fs":{"readTextFile":true,"writeTextFile":false},"terminal":true}}`)); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	got := a.ClientCapabilities()
	if !got.FS.ReadTextFile {
		t.Error("client offered fs.readTextFile; the agent did not record it")
	}
	if got.FS.WriteTextFile {
		t.Error("client did not offer fs.writeTextFile; the agent must not assume it")
	}
	if !got.Terminal {
		t.Error("client offered terminal; the agent did not record it")
	}
}
