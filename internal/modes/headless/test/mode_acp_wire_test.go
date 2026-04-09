package headless_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/modes/headless"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// TestHeadlessModeACPWiring verifies that headless mode can be wired to use ACP.
// This test simulates what runHeadless() does in cmd/rubichan/main.go.
func TestHeadlessModeACPWiring(t *testing.T) {
	// Create minimal config
	cfg := &config.Config{
		Provider: config.ProviderConfig{
			Default: "anthropic",
			Model:   "claude-3-5-sonnet-20241022",
		},
		Agent: config.AgentConfig{
			MaxTurns: 3,
		},
	}

	// Create a mock provider
	mockProvider := &mockLLMProvider{}

	// Create tool registry
	registry := tools.NewRegistry()

	// Create approval function (headless auto-approves)
	approvalFunc := func(ctx context.Context, tool string, input json.RawMessage) (bool, error) {
		return true, nil // auto-approve for headless
	}

	// Build agent options with ACP enabled (simulating runHeadless())
	var opts []agent.AgentOption
	opts = append(opts, agent.WithMode("headless"))
	opts = append(opts, agent.WithACP()) // This is the key line from main.go

	// Create agent
	agentCore := agent.New(mockProvider, registry, approvalFunc, cfg, opts...)
	if agentCore == nil {
		t.Fatal("failed to create agent")
	}

	// Verify ACP server is initialized
	acpServer := agentCore.ACPServer()
	if acpServer == nil {
		t.Fatal("ACP server not initialized in headless mode")
	}

	// Create headless ACP client (which creates its own server for external communication)
	client := headless.NewACPClient()
	if client == nil {
		t.Fatal("failed to create headless ACP client")
	}
	defer client.Close()

	// Verify client was created successfully
	t.Log("headless ACP client created successfully")
}

// mockLLMProvider is a minimal LLM provider for testing
type mockLLMProvider struct{}

func (m *mockLLMProvider) Stream(
	ctx context.Context,
	req agentsdk.CompletionRequest,
) (<-chan agentsdk.StreamEvent, error) {
	ch := make(chan agentsdk.StreamEvent)
	close(ch)
	return ch, nil
}
