package interactive_test

import (
	"sync"
	"testing"

	"github.com/julianshen/rubichan/internal/acp"
	"github.com/julianshen/rubichan/internal/modes/interactive"
)

// TestACPClientInitializeStructure tests that ACPClient can be created and initialized.
// Full transport integration testing is done in integration tests.
func TestACPClientInitializeStructure(t *testing.T) {
	// Create a minimal server
	registry := acp.NewCapabilityRegistry()
	server := acp.NewServer(registry)

	// Create client
	client := interactive.NewACPClient(server)
	defer client.Close()

	// Verify client was created correctly
	if client == nil {
		t.Fatal("client is nil")
	}
}

func TestACPClientConcurrentIDGeneration(t *testing.T) {
	// Create a minimal server
	registry := acp.NewCapabilityRegistry()
	server := acp.NewServer(registry)

	// Create client
	client := interactive.NewACPClient(server)
	defer client.Close()

	// Test that getNextID is thread-safe
	const numGoroutines = 10
	const requestsPerGoroutine = 100

	var wg sync.WaitGroup
	idChan := make(chan int64, numGoroutines*requestsPerGoroutine)

	// Spawn concurrent goroutines to get IDs
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				id := client.GetNextID()
				idChan <- id
			}
		}(i)
	}

	wg.Wait()
	close(idChan)

	// Verify all IDs are unique
	seen := make(map[int64]bool)
	for id := range idChan {
		if seen[id] {
			t.Errorf("duplicate ID: %d", id)
		}
		seen[id] = true
	}

	if len(seen) != numGoroutines*requestsPerGoroutine {
		t.Errorf("expected %d unique IDs, got %d", numGoroutines*requestsPerGoroutine, len(seen))
	}
}

func TestACPClientInitializeReturnsResponse(t *testing.T) {
	// This test requires the full transport loop to work.
	// For now, just verify the client structure is correct.
	registry := acp.NewCapabilityRegistry()
	server := acp.NewServer(registry)

	client := interactive.NewACPClient(server)
	defer client.Close()

	// Full end-to-end test will be added once the dispatcher listener is fully integrated
	t.Skip("waiting for full dispatcher listener integration")
}

func TestACPClientPrompt(t *testing.T) {
	// This test requires the full transport loop to work.
	// For now, just verify the client structure is correct.
	registry := acp.NewCapabilityRegistry()
	server := acp.NewServer(registry)

	client := interactive.NewACPClient(server)
	defer client.Close()

	t.Skip("waiting for full dispatcher listener integration")
}

func TestACPClientExecuteTool(t *testing.T) {
	// This test requires the full transport loop to work.
	// For now, just verify the client structure is correct.
	registry := acp.NewCapabilityRegistry()
	server := acp.NewServer(registry)

	client := interactive.NewACPClient(server)
	defer client.Close()

	t.Skip("waiting for full dispatcher listener integration")
}

func TestACPClientInvokeSkill(t *testing.T) {
	// This test requires the full transport loop to work.
	// For now, just verify the client structure is correct.
	registry := acp.NewCapabilityRegistry()
	server := acp.NewServer(registry)

	client := interactive.NewACPClient(server)
	defer client.Close()

	t.Skip("waiting for full dispatcher listener integration")
}

func TestACPClientApprovalRequest(t *testing.T) {
	// This test requires the full transport loop to work.
	// For now, just verify the client structure is correct.
	registry := acp.NewCapabilityRegistry()
	server := acp.NewServer(registry)

	client := interactive.NewACPClient(server)
	defer client.Close()

	t.Skip("waiting for full dispatcher listener integration")
}
