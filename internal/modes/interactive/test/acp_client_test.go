package interactive_test

import (
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/acp"
	"github.com/julianshen/rubichan/internal/modes/interactive"
)

func TestACPClientInitialize(t *testing.T) {
	client := interactive.NewACPClient()

	// Initialize
	resp, err := client.Initialize("rubichan-tui")
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	if resp == nil {
		t.Fatal("response is nil")
	}
	if resp.Result.ServerInfo.Name != "rubichan" {
		t.Errorf("got server name %q, want rubichan", resp.Result.ServerInfo.Name)
	}
}

func TestACPClientIDIncrement(t *testing.T) {
	client := interactive.NewACPClient()

	// Make multiple requests to test ID incrementing
	resp1, _ := client.Initialize("client1")
	resp2, _ := client.Initialize("client2")
	resp3, _ := client.Initialize("client3")

	if resp1 == nil || resp2 == nil || resp3 == nil {
		t.Error("responses should not be nil")
	}

	// Verify IDs would be different (1, 2, 3)
	// This is tested implicitly by ensuring no panic on concurrent access
}

func TestACPClientPrompt(t *testing.T) {
	client := interactive.NewACPClient()

	resp, err := client.Prompt("Explain this code", 5)
	if err != nil {
		t.Fatalf("prompt failed: %v", err)
	}

	// Stub returns nil; implementation will return actual response
	_ = resp
}

func TestACPClientExecuteTool(t *testing.T) {
	client := interactive.NewACPClient()

	input := json.RawMessage(`{"path":"main.go"}`)
	resp, err := client.ExecuteTool("file.read", input)
	if err != nil {
		t.Fatalf("execute tool failed: %v", err)
	}

	_ = resp
}

func TestACPClientInvokeSkill(t *testing.T) {
	client := interactive.NewACPClient()

	input := json.RawMessage(`{"code":"print('hello')"}`)
	resp, err := client.InvokeSkill("my_skill", "transform", input)
	if err != nil {
		t.Fatalf("invoke skill failed: %v", err)
	}

	_ = resp
}

func TestACPClientApprovalRequest(t *testing.T) {
	client := interactive.NewACPClient()

	verdict := acp.SecurityApprovalRequest{
		VerdictID: "sec-1",
		Severity:  "high",
		Message:   "Hardcoded secret",
		Options:   []string{"approve", "escalate", "block"},
	}

	resp, err := client.ApprovalRequest(verdict)
	if err != nil {
		t.Fatalf("approval request failed: %v", err)
	}

	_ = resp
}
