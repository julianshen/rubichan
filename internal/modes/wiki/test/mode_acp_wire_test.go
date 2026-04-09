package wiki_test

import (
	"testing"

	"github.com/julianshen/rubichan/internal/modes/wiki"
)

// TestWikiModeACPClient verifies that wiki mode can create an ACP client.
// Wiki mode does not use an agent loop directly; instead, it uses a
// separate wiki.Run() pipeline. The ACP client would be used if wiki
// functionality needs to be exposed via ACP in the future.
func TestWikiModeACPClient(t *testing.T) {
	// Create wiki ACP client (which creates its own server)
	client := wiki.NewACPClient()
	if client == nil {
		t.Fatal("failed to create wiki ACP client")
	}
	defer client.Close()

	t.Log("wiki ACP client created successfully")
}
