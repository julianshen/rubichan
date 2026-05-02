package toolexec

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

func TestPartitionToolCalls(t *testing.T) {
	// Mock tools: safe tools return true for IsConcurrencySafe,
	// unsafe tools return false.
	safe := &mockTool{name: "read", safe: true}
	unsafe := &mockTool{name: "write", safe: false}
	lookup := &mockLookup{
		tools: map[string]agentsdk.Tool{
			"read":  safe,
			"write": unsafe,
		},
	}

	calls := []ToolCall{
		{Name: "read", Input: []byte(`{}`)},
		{Name: "read", Input: []byte(`{}`)},
		{Name: "write", Input: []byte(`{}`)},
		{Name: "read", Input: []byte(`{}`)},
	}

	batches := partitionToolCalls(lookup, calls)
	if len(batches) != 3 {
		t.Fatalf("expected 3 batches, got %d", len(batches))
	}
	if !batches[0].IsConcurrent {
		t.Error("batch 0 should be concurrent")
	}
	if len(batches[0].Calls) != 2 {
		t.Errorf("batch 0 should have 2 calls, got %d", len(batches[0].Calls))
	}
	if batches[1].IsConcurrent {
		t.Error("batch 1 should be sequential")
	}
	if !batches[2].IsConcurrent {
		t.Error("batch 2 should be concurrent (read after unsafe is a new safe batch)")
	}
}

func TestPartitionToolCalls_Empty(t *testing.T) {
	lookup := &mockLookup{tools: map[string]agentsdk.Tool{}}
	batches := partitionToolCalls(lookup, nil)
	if batches != nil {
		t.Error("expected nil for empty calls")
	}
}

func TestPartitionToolCalls_UnknownTool(t *testing.T) {
	lookup := &mockLookup{tools: map[string]agentsdk.Tool{}}
	calls := []ToolCall{{Name: "unknown", Input: []byte(`{}`)}}
	batches := partitionToolCalls(lookup, calls)
	if len(batches) != 1 {
		t.Fatalf("expected 1 batch, got %d", len(batches))
	}
	if batches[0].IsConcurrent {
		t.Error("unknown tool should be treated as unsafe (fail-closed)")
	}
}

func TestBatchExecutor(t *testing.T) {
	// safe tool sleeps 50ms, unsafe sleeps 100ms
	safe := &mockTool{name: "safe", safe: true, delay: 50 * time.Millisecond}
	unsafe := &mockTool{name: "unsafe", safe: false, delay: 100 * time.Millisecond}
	lookup := &mockLookup{tools: map[string]agentsdk.Tool{"safe": safe, "unsafe": unsafe}}

	calls := []ToolCall{
		{Name: "safe"},
		{Name: "safe"},
		{Name: "unsafe"},
	}

	exec := NewBatchExecutor(lookup, RegistryExecutor(lookup), 10)
	start := time.Now()
	results := exec.Execute(context.Background(), calls)
	elapsed := time.Since(start)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// Two safe tools in parallel should take ~50ms, then unsafe ~100ms.
	// Total should be < 200ms (sequential would be 200ms).
	if elapsed > 180*time.Millisecond {
		t.Errorf("expected parallel execution, took %v", elapsed)
	}
}

func TestBatchExecutor_ContextCancellation(t *testing.T) {
	safe := &mockTool{name: "safe", safe: true, delay: 100 * time.Millisecond}
	lookup := &mockLookup{tools: map[string]agentsdk.Tool{"safe": safe}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancel

	exec := NewBatchExecutor(lookup, RegistryExecutor(lookup), 10)
	calls := []ToolCall{{Name: "safe"}, {Name: "safe"}}
	results := exec.Execute(ctx, calls)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for i, r := range results {
		if !r.IsError {
			t.Errorf("result %d: expected error due to cancellation", i)
		}
	}
}

// mockTool implements agentsdk.Tool and agentsdk.ConcurrencySafeTool for testing.
type mockTool struct {
	name  string
	safe  bool
	delay time.Duration
}

func (m *mockTool) Name() string                 { return m.name }
func (m *mockTool) Description() string          { return "" }
func (m *mockTool) InputSchema() json.RawMessage { return []byte(`{}`) }
func (m *mockTool) Execute(ctx context.Context, input json.RawMessage) (agentsdk.ToolResult, error) {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	return agentsdk.ToolResult{Content: m.name}, nil
}
func (m *mockTool) IsConcurrencySafe() bool { return m.safe }

type mockLookup struct {
	tools map[string]agentsdk.Tool
}

func (m *mockLookup) Get(name string) (agentsdk.Tool, bool) {
	t, ok := m.tools[name]
	return t, ok
}
