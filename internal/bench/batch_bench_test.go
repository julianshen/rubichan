package bench

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// mockBenchTool implements agentsdk.Tool for benchmarking.
type mockBenchTool struct {
	name  string
	safe  bool
	delay time.Duration
}

func (m *mockBenchTool) Name() string                 { return m.name }
func (m *mockBenchTool) Description() string          { return "" }
func (m *mockBenchTool) InputSchema() json.RawMessage { return []byte(`{}`) }
func (m *mockBenchTool) Execute(ctx context.Context, input json.RawMessage) (agentsdk.ToolResult, error) {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	return agentsdk.ToolResult{Content: m.name}, nil
}
func (m *mockBenchTool) IsConcurrencySafe() bool { return m.safe }

type benchLookup struct {
	tools map[string]agentsdk.Tool
}

func (m *benchLookup) Get(name string) (agentsdk.Tool, bool) {
	t, ok := m.tools[name]
	return t, ok
}

func BenchmarkBatchExecutor_ParallelSafe(b *testing.B) {
	read := &mockBenchTool{name: "read", safe: true, delay: 1 * time.Millisecond}
	lookup := &benchLookup{tools: map[string]agentsdk.Tool{"read": read}}
	calls := make([]toolexec.ToolCall, 10)
	for i := range calls {
		calls[i] = toolexec.ToolCall{Name: "read", Input: []byte(`{}`)}
	}
	handler := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "ok"}
	}
	exec := toolexec.NewBatchExecutor(lookup, handler, 10)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = exec.Execute(context.Background(), calls)
	}
}

func BenchmarkBatchExecutor_SerialUnsafe(b *testing.B) {
	write := &mockBenchTool{name: "write", safe: false, delay: 1 * time.Millisecond}
	lookup := &benchLookup{tools: map[string]agentsdk.Tool{"write": write}}
	calls := make([]toolexec.ToolCall, 10)
	for i := range calls {
		calls[i] = toolexec.ToolCall{Name: "write", Input: []byte(`{}`)}
	}
	handler := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "ok"}
	}
	exec := toolexec.NewBatchExecutor(lookup, handler, 10)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = exec.Execute(context.Background(), calls)
	}
}

func BenchmarkBatchExecutor_Mixed(b *testing.B) {
	read := &mockBenchTool{name: "read", safe: true, delay: 1 * time.Millisecond}
	write := &mockBenchTool{name: "write", safe: false, delay: 1 * time.Millisecond}
	lookup := &benchLookup{tools: map[string]agentsdk.Tool{"read": read, "write": write}}
	calls := make([]toolexec.ToolCall, 20)
	for i := range calls {
		if i%3 == 0 {
			calls[i] = toolexec.ToolCall{Name: "write", Input: []byte(`{}`)}
		} else {
			calls[i] = toolexec.ToolCall{Name: "read", Input: []byte(`{}`)}
		}
	}
	handler := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "ok"}
	}
	exec := toolexec.NewBatchExecutor(lookup, handler, 10)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = exec.Execute(context.Background(), calls)
	}
}

func BenchmarkBatchExecutor_LargeBatch(b *testing.B) {
	read := &mockBenchTool{name: "read", safe: true, delay: 100 * time.Microsecond}
	lookup := &benchLookup{tools: map[string]agentsdk.Tool{"read": read}}
	calls := make([]toolexec.ToolCall, 50)
	for i := range calls {
		calls[i] = toolexec.ToolCall{Name: "read", Input: []byte(`{}`)}
	}
	handler := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "ok"}
	}
	exec := toolexec.NewBatchExecutor(lookup, handler, 10)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = exec.Execute(context.Background(), calls)
	}
}

func BenchmarkBatchExecutor_SiblingAbort(b *testing.B) {
	shell := &mockBenchTool{name: "shell", safe: true, delay: 500 * time.Microsecond}
	read := &mockBenchTool{name: "read", safe: true, delay: 2 * time.Millisecond}
	lookup := &benchLookup{tools: map[string]agentsdk.Tool{"shell": shell, "read": read}}
	calls := []toolexec.ToolCall{{Name: "shell"}, {Name: "read"}}
	handler := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		if tc.Name == "shell" {
			return toolexec.Result{Content: "error", IsError: true}
		}
		return toolexec.Result{Content: "ok"}
	}
	exec := toolexec.NewBatchExecutor(lookup, handler, 10)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = exec.Execute(context.Background(), calls)
	}
}
