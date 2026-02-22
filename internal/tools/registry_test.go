package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTool implements the Tool interface for testing.
type mockTool struct {
	name        string
	description string
	inputSchema json.RawMessage
	executeFn   func(ctx context.Context, input json.RawMessage) (ToolResult, error)
}

func (m *mockTool) Name() string                 { return m.name }
func (m *mockTool) Description() string          { return m.description }
func (m *mockTool) InputSchema() json.RawMessage { return m.inputSchema }
func (m *mockTool) Execute(ctx context.Context, input json.RawMessage) (ToolResult, error) {
	if m.executeFn != nil {
		return m.executeFn(ctx, input)
	}
	return ToolResult{Content: "ok"}, nil
}

func newMockTool(name, description string) *mockTool {
	return &mockTool{
		name:        name,
		description: description,
		inputSchema: json.RawMessage(`{"type":"object"}`),
	}
}

func TestRegistryRegisterAndGet(t *testing.T) {
	reg := NewRegistry()
	tool := newMockTool("test_tool", "A test tool")

	err := reg.Register(tool)
	require.NoError(t, err)

	got, ok := reg.Get("test_tool")
	assert.True(t, ok)
	assert.Equal(t, "test_tool", got.Name())
	assert.Equal(t, "A test tool", got.Description())
}

func TestRegistryDuplicateReturnsError(t *testing.T) {
	reg := NewRegistry()
	tool := newMockTool("dup_tool", "First registration")

	err := reg.Register(tool)
	require.NoError(t, err)

	tool2 := newMockTool("dup_tool", "Second registration")
	err = reg.Register(tool2)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tool already registered: dup_tool")
}

func TestRegistryGetMissing(t *testing.T) {
	reg := NewRegistry()

	got, ok := reg.Get("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestRegistryRegisterNil(t *testing.T) {
	reg := NewRegistry()
	err := reg.Register(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot register nil tool")
}

func TestRegistryUnregister(t *testing.T) {
	reg := NewRegistry()
	tool := newMockTool("removable", "Will be removed")
	require.NoError(t, reg.Register(tool))

	// Tool exists.
	_, ok := reg.Get("removable")
	assert.True(t, ok)

	// Unregister it.
	err := reg.Unregister("removable")
	require.NoError(t, err)

	// No longer exists.
	_, ok = reg.Get("removable")
	assert.False(t, ok)
}

func TestRegistryUnregisterMissing(t *testing.T) {
	reg := NewRegistry()
	err := reg.Unregister("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tool not registered: nonexistent")
}

func TestRegistryAll(t *testing.T) {
	reg := NewRegistry()
	tool1 := newMockTool("tool_a", "Tool A")
	tool2 := newMockTool("tool_b", "Tool B")

	require.NoError(t, reg.Register(tool1))
	require.NoError(t, reg.Register(tool2))

	defs := reg.All()
	assert.Len(t, defs, 2)

	// Collect names from the returned defs
	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Name] = true
		assert.NotEmpty(t, d.Description)
		assert.NotNil(t, d.InputSchema)
	}
	assert.True(t, names["tool_a"])
	assert.True(t, names["tool_b"])
}
