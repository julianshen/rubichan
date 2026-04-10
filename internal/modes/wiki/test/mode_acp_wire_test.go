package wiki_test

import (
	"testing"

	"github.com/julianshen/rubichan/internal/acp"
	"github.com/julianshen/rubichan/internal/modes/wiki"
)

// TestWikiModeACPClient verifies that wiki mode can create an ACP client.
// Wiki mode does not use an agent loop directly; instead, it uses a
// separate wiki.Run() pipeline. The ACP client would be used if wiki
// functionality needs to be exposed via ACP in the future.
func TestWikiModeACPClient(t *testing.T) {
	// Create wiki ACP client with a server instance
	registry := acp.NewCapabilityRegistry()
	server := acp.NewServer(registry)

	client, err := wiki.NewACPClient(server)
	if err != nil {
		t.Fatalf("failed to create wiki ACP client: %v", err)
	}
	defer client.Close()

	t.Log("wiki ACP client created successfully")
}
