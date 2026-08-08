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

	// A registered method must be routed — a handler error is fine, but
	// ErrMethodNotFound means it was never wired. This checks initialize
	// because it is now the only method NewACPRegistry registers: the session
	// methods come from serveACP, and the skill/security stubs were removed for
	// answering with fabricated success.
	if _, err := registry.Call("initialize",
		json.RawMessage(`{"protocolVersion":1,"clientCapabilities":{}}`)); errors.Is(err, acp.ErrMethodNotFound) {
		t.Error("initialize not registered on the composed registry")
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

// TestNewACPRegistryServesNothingItCannotBack is the gate that made shipping
// --acp safe. Until a mode actually served this registry, five registered
// methods fabricated success and nobody could reach them:
//
//	security/scan     answered "no findings" without scanning anything
//	security/approve  accepted a decision and recorded it nowhere
//	skill/invoke      reported success for any skill name
//	skill/list        reported an empty catalogue as fact
//	skill/manifest    reported any name as "loaded"
//
// A security scan that answers "clean" without scanning does not fail, it
// reassures — which is worse than not answering. Two more had to go with them:
// agent/prompt ran a turn straight off the registry, bypassing the handshake
// gate, the cwd check and the capability gate that session/prompt enforces;
// tool/execute returned a successful JSON-RPC response whose payload said
// "not_implemented", which is a lie told in a success envelope.
//
// A method that is absent answers -32601 and tells the client the truth.
func TestNewACPRegistryServesNothingItCannotBack(t *testing.T) {
	registry := agent.NewACPRegistry(createTestAgent(t))

	for _, method := range []string{
		"security/scan",
		"security/approve",
		"skill/invoke",
		"skill/list",
		"skill/manifest",
		"agent/prompt",
		"tool/execute",
	} {
		_, err := registry.Call(method, json.RawMessage(`{}`))
		if !errors.Is(err, acp.ErrMethodNotFound) {
			t.Errorf("%s is still registered: an unimplemented method must be absent, not answer with a fabricated success (got %v)", method, err)
		}
	}

	// The honest set stays reachable; this is not a blanket teardown. Only
	// initialize is checked here: the session methods are registered by
	// serveACP once a connection exists, because their prompt handler streams
	// through it.
	if _, err := registry.Call("initialize", json.RawMessage(`{}`)); errors.Is(err, acp.ErrMethodNotFound) {
		t.Error("initialize must remain registered")
	}
}
