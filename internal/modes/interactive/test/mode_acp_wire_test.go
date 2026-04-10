package interactive_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/modes/interactive"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// TestInteractiveModeACPWiring verifies that interactive mode can be wired to use ACP.
// This test simulates what runInteractive() does in cmd/rubichan/main.go.
func TestInteractiveModeACPWiring(t *testing.T) {
	// Create minimal config
	cfg := &config.Config{
		Provider: config.ProviderConfig{
			Default: "anthropic",
			Model:   "claude-3-5-sonnet-20241022",
		},
		Agent: config.AgentConfig{
			MaxTurns: 5,
		},
	}

	// Create a mock provider
	mockProvider := &mockLLMProvider{}

	// Create tool registry
	registry := tools.NewRegistry()

	// Create approval function
	approvalFunc := func(ctx context.Context, tool string, input json.RawMessage) (bool, error) {
		return true, nil // auto-approve for test
	}

	// Build agent options with ACP enabled (simulating runInteractive())
	var opts []agent.AgentOption
	opts = append(opts, agent.WithMode("interactive"))
	opts = append(opts, agent.WithACP()) // This is the key line from main.go

	// Create agent
	agentCore := agent.New(mockProvider, registry, approvalFunc, cfg, opts...)
	if agentCore == nil {
		t.Fatal("failed to create agent")
	}

	// Verify ACP server is initialized
	acpServer := agentCore.ACPServer()
	if acpServer == nil {
		t.Fatal("ACP server not initialized in interactive mode")
	}

	// Create interactive ACP client (simulating what the TUI would do)
	client, err := interactive.NewACPClient(nil, "", acpServer)
	if err != nil {
		t.Fatalf("failed to create interactive ACP client: %v", err)
	}
	defer client.Close()

	// Verify client was created successfully
	t.Log("interactive ACP client created successfully with ACP server")
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
