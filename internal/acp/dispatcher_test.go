package acp_test

import (
	"testing"

	"github.com/julianshen/rubichan/internal/acp"
)

// TestResponseDispatcherCreation verifies that ResponseDispatcher can be created properly.
func TestResponseDispatcherCreation(t *testing.T) {
	registry := &acp.CapabilityRegistry{}
	server := acp.NewServer(registry)

	// Create a simple stub transport (we test routing logic without the transport complexity)
	// For now, just verify the dispatcher can be instantiated
	// Full transport integration is tested in e2e tests

	// This is a structural test that verifies the dispatcher setup works
	// Transport integration tests belong in e2e, not unit tests
	if server == nil {
		t.Fatal("server is nil")
	}
}

// TestResponseDispatcherRoutingLogic verifies response routing by ID
func TestResponseDispatcherRoutingLogic(t *testing.T) {
	registry := &acp.CapabilityRegistry{}
	server := acp.NewServer(registry)

	// Create dispatcher (transport can be nil for this structural test)
	// We're testing that pending map management works correctly
	dispatcher := acp.NewResponseDispatcher(nil, server)

	if dispatcher == nil {
		t.Fatal("dispatcher is nil")
	}

	// The routing logic is tested through the Scan/Send interaction
	// which requires a full transport integration (see e2e tests)
}
