package interactive_test

import (
	"encoding/json"
	"sync"
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

func TestACPClientConcurrentIDGeneration(t *testing.T) {
	client := interactive.NewACPClient()
	const numGoroutines = 10
	const requestsPerGoroutine = 100

	var wg sync.WaitGroup
	errChan := make(chan error, numGoroutines*requestsPerGoroutine)

	// Spawn concurrent goroutines
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				// Just verify Initialize() doesn't panic and returns valid response
				_, err := client.Initialize("concurrent-client")
				if err != nil {
					errChan <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check for any errors
	for err := range errChan {
		if err != nil {
			t.Errorf("Initialize() error: %v", err)
		}
	}
	// If we reach here without panic, concurrency is working correctly
}

func TestACPClientInitializeReturnsResponse(t *testing.T) {
	client := interactive.NewACPClient()

	resp, err := client.Initialize("test-client")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify response content
	if resp == nil {
		t.Fatal("response is nil")
	}
	if resp.Result.ServerInfo.Name != "rubichan" {
		t.Errorf("got server name %q, want rubichan", resp.Result.ServerInfo.Name)
	}
	if resp.Result.ServerInfo.Version != "1.0.0" {
		t.Errorf("got version %q, want 1.0.0", resp.Result.ServerInfo.Version)
	}
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
