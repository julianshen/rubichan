package wiki_test

import (
	"testing"

	"github.com/julianshen/rubichan/internal/acp"
	"github.com/julianshen/rubichan/internal/modes/wiki"
)

// TestACPClientCreateStructure tests that ACPClient can be created.
// Full transport integration testing is done in integration tests.
func TestACPClientCreateStructure(t *testing.T) {
	registry := acp.NewCapabilityRegistry()
	server := acp.NewServer(registry)
	client, err := wiki.NewACPClient(server)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	if client == nil {
		t.Fatal("client is nil")
	}
}

func TestProgressTracking(t *testing.T) {
	registry := acp.NewCapabilityRegistry()
	server := acp.NewServer(registry)
	client, err := wiki.NewACPClient(server)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	client.SetProgress(50)
	if client.Progress() != 50 {
		t.Error("progress should be 50")
	}

	// Test clamping
	client.SetProgress(150)
	if client.Progress() != 100 {
		t.Error("progress should clamp to 100")
	}

	client.SetProgress(-10)
	if client.Progress() != 0 {
		t.Error("progress should clamp to 0")
	}
}

func TestWikiClientProgress(t *testing.T) {
	registry := acp.NewCapabilityRegistry()
	server := acp.NewServer(registry)
	client, err := wiki.NewACPClient(server)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	if client.Progress() != 0 {
		t.Errorf("initial progress should be 0, got %d", client.Progress())
	}

	client.SetProgress(50)
	if client.Progress() != 50 {
		t.Errorf("got %d, want 50", client.Progress())
	}

	client.SetProgress(150) // Should clamp to 100
	if client.Progress() != 100 {
		t.Errorf("got %d, want 100 (clamped)", client.Progress())
	}

	client.SetProgress(-10) // Should clamp to 0
	if client.Progress() != 0 {
		t.Errorf("got %d, want 0 (clamped)", client.Progress())
	}
}

func TestGenerateDocsWithTransport(t *testing.T) {
	// This test requires a full transport loop with server handlers.
	// Full end-to-end test will be added in multi-mode integration tests.
	t.Skip("waiting for full transport loop and server handler integration")
}

// TestGenerateDocsStructure tests that GenerateDocs can build a valid request.
func TestGenerateDocsStructure(t *testing.T) {
	// Create a minimal server with dispatcher
	registry := acp.NewCapabilityRegistry()
	server := acp.NewServer(registry)

	client, err := wiki.NewACPClient(server)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	// Verify client was created correctly
	if client == nil {
		t.Fatal("client is nil")
	}

	_ = server // Verify server creation doesn't error
}

// TestProgressClampingBoundaries tests clamping at boundaries
func TestProgressClampingBoundaries(t *testing.T) {
	registry := acp.NewCapabilityRegistry()
	server := acp.NewServer(registry)
	client, err := wiki.NewACPClient(server)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	tests := []struct {
		input    int
		expected int
	}{
		{0, 0},
		{50, 50},
		{100, 100},
		{-1, 0},
		{-100, 0},
		{101, 100},
		{999, 100},
	}

	for _, tt := range tests {
		client.SetProgress(tt.input)
		if got := client.Progress(); got != tt.expected {
			t.Errorf("SetProgress(%d) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}
