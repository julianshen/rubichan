package agentsdk

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type dummyTool struct {
	name string
}

func (d *dummyTool) Name() string                 { return d.name }
func (d *dummyTool) Description() string          { return "a dummy tool" }
func (d *dummyTool) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (d *dummyTool) Execute(_ context.Context, _ json.RawMessage) (ToolResult, error) {
	return ToolResult{Content: "ok"}, nil
}

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	err := r.Register(&dummyTool{name: "foo"})
	require.NoError(t, err)

	tool, ok := r.Get("foo")
	assert.True(t, ok)
	assert.Equal(t, "foo", tool.Name())
}

func TestRegistryRegisterDuplicate(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&dummyTool{name: "foo"}))
	err := r.Register(&dummyTool{name: "foo"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestRegistryRegisterNil(t *testing.T) {
	r := NewRegistry()
	err := r.Register(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil tool")
}

func TestRegistryUnregister(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&dummyTool{name: "foo"}))
	require.NoError(t, r.Unregister("foo"))
	_, ok := r.Get("foo")
	assert.False(t, ok)
}

func TestRegistryUnregisterNotFound(t *testing.T) {
	r := NewRegistry()
	err := r.Unregister("missing")
	assert.Error(t, err)
}

func TestRegistryAll(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&dummyTool{name: "a"}))
	require.NoError(t, r.Register(&dummyTool{name: "b"}))

	defs := r.All()
	assert.Len(t, defs, 2)
}

func TestRegistryCount(t *testing.T) {
	r := NewRegistry()
	assert.Equal(t, 0, r.Count())
	require.NoError(t, r.Register(&dummyTool{name: "x"}))
	assert.Equal(t, 1, r.Count())
}

func TestRegistryGetNotFound(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("missing")
	assert.False(t, ok)
}
