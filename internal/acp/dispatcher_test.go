package acp_test

import (
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/acp"
)

func TestDispatcherSendsAndReceivesRequest(t *testing.T) {
	// Create a mock transport that echoes requests back as responses
	req := acp.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "test",
		Params:  json.RawMessage(`{"key":"value"}`),
	}

	// Dispatcher should be able to send and get response
	// This test will fail until dispatcher exists
	_ = req
	t.Skip("dispatcher not yet implemented")
}

func TestDispatcherRoutesResponses(t *testing.T) {
	// This test verifies that the dispatcher correctly routes responses to waiting callers
	// Detailed implementation after Step 5
	t.Skip("listener implementation pending")
}
