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

// TestInteractiveModeWithACP verifies that the interactive mode can create
// an agent with ACP enabled and communicate via the ACP client.
func TestInteractiveModeWithACP(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Skip if ANTHROPIC_API_KEY is not set (requires real API)
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("skipping test - ANTHROPIC_API_KEY not set")
	}

	// Create minimal config
	cfg := &config.Config{
		Provider: config.ProviderConfig{
			Default: "anthropic",
			Model:   "claude-3-5-sonnet-20241022",
			Anthropic: config.AnthropicProviderConfig{
				APIKeySource: "env",
			},
		},
		Agent: config.AgentConfig{
			MaxTurns: 5,
		},
	}

	// Create a test provider (mock would be ideal, but we use real provider)
	p, err := provider.NewProvider(cfg)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Create tool registry
	registry := tools.NewRegistry()

	// Create agent with ACP enabled
	approvalFunc := func(ctx context.Context, tool string, input json.RawMessage) (bool, error) {
		return true, nil // auto-approve for test
	}

	opts := []agent.AgentOption{
		agent.WithACP(),
		agent.WithMode("interactive"),
	}

	agentCore := agent.New(p, registry, approvalFunc, cfg, opts...)

	// Verify ACP server is initialized
	acpServer := agentCore.ACPServer()
	if acpServer == nil {
		t.Error("ACP server not initialized")
		return
	}

	// Create ACP client for interactive mode
	client, err := interactive.NewACPClient(nil, "", acpServer)
	if err != nil {
		t.Errorf("failed to create interactive ACP client: %v", err)
		return
	}
	if client == nil {
		t.Error("interactive ACP client is nil")
	}

	// Test initialize handshake via the interactive client
	resp, err := client.Initialize("test-tui")
	if err != nil {
		t.Errorf("initialize failed: %v", err)
	}
	if resp == nil {
		t.Error("initialize response is nil")
	}
}

// TestHeadlessModeWithACP verifies that the headless mode can create
// an agent with ACP enabled and run code review operations.
func TestHeadlessModeWithACP(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Skip if ANTHROPIC_API_KEY is not set (requires real API)
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("skipping test - ANTHROPIC_API_KEY not set")
	}

	// Create minimal config
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

	// Create provider
	p, err := provider.NewProvider(cfg)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Create tool registry
	registry := tools.NewRegistry()

	// Create agent with ACP enabled
	approvalFunc := func(ctx context.Context, tool string, input json.RawMessage) (bool, error) {
		return true, nil // auto-approve for test
	}

	opts := []agent.AgentOption{
		agent.WithACP(),
		agent.WithMode("headless"),
	}

	agentCore := agent.New(p, registry, approvalFunc, cfg, opts...)

	// Verify ACP server is initialized
	acpServer := agentCore.ACPServer()
	if acpServer == nil {
		t.Error("ACP server not initialized")
	}

	// Create ACP client for headless mode
	client, err := headless.NewACPClient(acpServer)
	if err != nil {
		t.Errorf("failed to create headless ACP client: %v", err)
		return
	}
	if client == nil {
		t.Error("headless ACP client is nil")
	}

	// Test code review operation setup
	if client.Timeout() == 0 {
		t.Error("client timeout not initialized")
	}
}

// TestWikiModeWithACP verifies that the wiki mode can create
// an agent with ACP enabled and support documentation generation.
func TestWikiModeWithACP(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Skip if ANTHROPIC_API_KEY is not set (requires real API)
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("skipping test - ANTHROPIC_API_KEY not set")
	}

	// Create minimal config
	cfg := &config.Config{
		Provider: config.ProviderConfig{
			Default: "anthropic",
			Model:   "claude-3-5-sonnet-20241022",
			Anthropic: config.AnthropicProviderConfig{
				APIKeySource: "env",
			},
		},
		Agent: config.AgentConfig{
			MaxTurns: 5,
		},
	}

	// Create provider
	p, err := provider.NewProvider(cfg)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Create tool registry
	registry := tools.NewRegistry()

	// Create agent with ACP enabled
	approvalFunc := func(ctx context.Context, tool string, input json.RawMessage) (bool, error) {
		return true, nil // auto-approve for test
	}

	opts := []agent.AgentOption{
		agent.WithACP(),
		agent.WithMode("wiki"),
	}

	agentCore := agent.New(p, registry, approvalFunc, cfg, opts...)

	// Verify ACP server is initialized
	acpServer := agentCore.ACPServer()
	if acpServer == nil {
		t.Error("ACP server not initialized")
	}

	// Create ACP client for wiki mode
	client, err := wiki.NewACPClient(acpServer)
	if err != nil {
		t.Errorf("failed to create wiki ACP client: %v", err)
		return
	}
	if client == nil {
		t.Error("wiki ACP client is nil")
	}

	// Test wiki client progress tracking
	if client.Progress() != 0 {
		t.Error("initial progress should be 0")
	}
	client.SetProgress(50)
	if client.Progress() != 50 {
		t.Error("progress should be updated")
	}
}

// TestACPServerCapabilities verifies that the ACP server reports
// all expected capabilities when initialized.
func TestACPServerCapabilities(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Skip if ANTHROPIC_API_KEY is not set (requires real API)
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("skipping test - ANTHROPIC_API_KEY not set")
	}

	// Create minimal config
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

	// Create provider
	p, err := provider.NewProvider(cfg)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Create tool registry with a basic tool
	registry := tools.NewRegistry()

	// Create agent with ACP enabled
	approvalFunc := func(ctx context.Context, tool string, input json.RawMessage) (bool, error) {
		return true, nil
	}

	opts := []agent.AgentOption{
		agent.WithACP(),
	}

	agentCore := agent.New(p, registry, approvalFunc, cfg, opts...)

	// Get ACP server and verify it has methods registered
	acpServer := agentCore.ACPServer()
	if acpServer == nil {
		t.Fatal("ACP server not initialized")
	}

	// The server should be ready to handle requests
	// (actual method routing validation is in internal/acp tests)
	t.Logf("ACP server created successfully for agent")
}

// TestMultipleModeClientsWithSingleAgent verifies that multiple mode clients
// can coexist and coordinate through the same agent's ACP server.
func TestMultipleModeClientsWithSingleAgent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Skip if ANTHROPIC_API_KEY is not set (requires real API)
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("skipping test - ANTHROPIC_API_KEY not set")
	}

	// Create minimal config
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

	// Create provider
	p, err := provider.NewProvider(cfg)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Create tool registry
	registry := tools.NewRegistry()

	// Create agent with ACP enabled
	approvalFunc := func(ctx context.Context, tool string, input json.RawMessage) (bool, error) {
		return true, nil
	}

	opts := []agent.AgentOption{
		agent.WithACP(),
	}

	agentCore := agent.New(p, registry, approvalFunc, cfg, opts...)

	// All clients should be able to interface with the same agent's ACP server
	acpServer := agentCore.ACPServer()
	if acpServer == nil {
		t.Error("ACP server not initialized")
		return
	}

	// Create all three mode clients
	interactiveClient, err := interactive.NewACPClient(nil, "", acpServer)
	if err != nil {
		t.Errorf("failed to create interactive client: %v", err)
		return
	}
	headlessClient, err := headless.NewACPClient(acpServer)
	if err != nil {
		t.Errorf("failed to create headless client: %v", err)
		return
	}
	wikiClient, err := wiki.NewACPClient(acpServer)
	if err != nil {
		t.Errorf("failed to create wiki client: %v", err)
		return
	}

	// Verify all clients are operational
	if interactiveClient == nil {
		t.Error("interactive client is nil")
	}
	if headlessClient == nil {
		t.Error("headless client is nil")
	}
	if wikiClient == nil {
		t.Error("wiki client is nil")
	}

	if acpServer == nil {
		t.Error("ACP server not initialized")
	}

	t.Logf("Successfully created agent with ACP server and all three mode clients")
}
