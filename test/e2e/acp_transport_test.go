package e2e_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/modes/headless"
	"github.com/julianshen/rubichan/internal/modes/interactive"
	"github.com/julianshen/rubichan/internal/modes/wiki"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/tools"

	// Register provider implementations
	_ "github.com/julianshen/rubichan/internal/provider/anthropic"
	_ "github.com/julianshen/rubichan/internal/provider/ollama"
	_ "github.com/julianshen/rubichan/internal/provider/openai"
	_ "github.com/julianshen/rubichan/internal/provider/zai"
)

func TestAllModeClientsWithTransport(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Skip if ANTHROPIC_API_KEY is not set (requires real API)
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("skipping test - ANTHROPIC_API_KEY not set")
	}

	// Create a shared agent core (would be done in mode entrypoints)
	cfg := &config.Config{
		Provider: config.ProviderConfig{
			Default: "anthropic",
			Model:   "claude-3-5-sonnet-20241022",
			Anthropic: config.AnthropicProviderConfig{
				APIKeySource: "env",
			},
		},
		Agent: config.AgentConfig{
			MaxTurns: 3,
		},
	}

	p, err := provider.NewProvider(cfg)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	registry := tools.NewRegistry()
	approvalFunc := func(ctx context.Context, tool string, input json.RawMessage) (bool, error) {
		return true, nil
	}

	agentCore := agent.New(p, registry, approvalFunc, cfg,
		agent.WithACP(),
	)

	// Verify ACP server exists
	acpServer := agentCore.ACPServer()
	if acpServer == nil {
		t.Fatal("ACP server not initialized")
	}

	// Test interactive client
	t.Run("InteractiveClientWithTransport", func(t *testing.T) {
		client, err := interactive.NewACPClient(acpServer)
		if err != nil {
			t.Fatalf("failed to create interactive client: %v", err)
		}
		defer client.Close()

		// Initialize
		resp, err := client.Initialize("test-interactive")
		if err != nil {
			t.Fatalf("Initialize failed: %v", err)
		}
		if resp == nil {
			t.Error("response is nil")
		}
	})

	// Test headless client
	t.Run("HeadlessClientWithTransport", func(t *testing.T) {
		client, err := headless.NewACPClient(acpServer)
		if err != nil {
			t.Fatalf("failed to create headless client: %v", err)
		}
		defer client.Close()

		// Test timeout getter/setter
		client.SetTimeout(5)
		if client.Timeout() != 5 {
			t.Error("timeout not set correctly")
		}
	})

	// Test wiki client
	t.Run("WikiClientWithTransport", func(t *testing.T) {
		client, err := wiki.NewACPClient(acpServer)
		if err != nil {
			t.Fatalf("failed to create wiki client: %v", err)
		}
		defer client.Close()

		// Test progress getter/setter
		client.SetProgress(25)
		if client.Progress() != 25 {
			t.Error("progress not set correctly")
		}
	})
}
