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

type hintedTool struct {
	dummyTool
	hint string
}

func (h *hintedTool) SearchHint() string { return h.hint }

func TestRegistryAliasResolution(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&dummyTool{name: "shell"}))
	require.NoError(t, r.RegisterAlias("bash", "shell"))

	tool, ok := r.Get("bash")
	require.True(t, ok)
	assert.Equal(t, "shell", tool.Name())
}

func TestRegistryAliasShadowingCanonicalName(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&dummyTool{name: "shell"}))
	err := r.RegisterAlias("shell", "other")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "shadows")
}

func TestRegistryAliasConflict(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&dummyTool{name: "shell"}))
	require.NoError(t, r.RegisterAlias("bash", "shell"))
	// Same target again is idempotent.
	require.NoError(t, r.RegisterAlias("bash", "shell"))
	// Different target is an error.
	err := r.RegisterAlias("bash", "file")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestRegistryAllExcludesAliases(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&dummyTool{name: "shell"}))
	require.NoError(t, r.RegisterAlias("bash", "shell"))

	defs := r.All()
	require.Len(t, defs, 1)
	assert.Equal(t, "shell", defs[0].Name)
}

func TestRegistryAllSortedByName(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&dummyTool{name: "zeta"}))
	require.NoError(t, r.Register(&dummyTool{name: "alpha"}))
	require.NoError(t, r.Register(&dummyTool{name: "mid"}))

	defs := r.All()
	require.Len(t, defs, 3)
	assert.Equal(t, []string{"alpha", "mid", "zeta"},
		[]string{defs[0].Name, defs[1].Name, defs[2].Name})
}

func TestRegistryAllIncludesSearchHint(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&hintedTool{dummyTool: dummyTool{name: "web"}, hint: "http fetch url"}))
	require.NoError(t, r.Register(&dummyTool{name: "plain"}))

	defs := r.All()
	require.Len(t, defs, 2)
	byName := map[string]ToolDef{}
	for _, d := range defs {
		byName[d.Name] = d
	}
	assert.Equal(t, "http fetch url", byName["web"].SearchHint)
	assert.Empty(t, byName["plain"].SearchHint)
}

func TestRegistryNames(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&dummyTool{name: "a"}))
	require.NoError(t, r.Register(&dummyTool{name: "b"}))
	require.NoError(t, r.RegisterAlias("alias-a", "a"))

	names := r.Names()
	assert.ElementsMatch(t, []string{"a", "b"}, names)
}

func TestRegistryFilter(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&dummyTool{name: "a"}))
	require.NoError(t, r.Register(&dummyTool{name: "b"}))
	require.NoError(t, r.Register(&dummyTool{name: "c"}))
	require.NoError(t, r.RegisterAlias("alias-a", "a"))
	require.NoError(t, r.RegisterAlias("alias-c", "c"))

	filtered := r.Filter([]string{"a", "b", "unknown"})
	assert.Equal(t, 2, filtered.Count())

	// Alias whose target survived is copied; alias to a dropped tool is not.
	tool, ok := filtered.Get("alias-a")
	require.True(t, ok)
	assert.Equal(t, "a", tool.Name())
	_, ok = filtered.Get("alias-c")
	assert.False(t, ok)

	// Filtered registry is independent of the original.
	require.NoError(t, filtered.Unregister("a"))
	_, ok = r.Get("a")
	assert.True(t, ok)
}

func TestRegistryFilterNilCopiesAll(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&dummyTool{name: "a"}))
	require.NoError(t, r.Register(&dummyTool{name: "b"}))
	require.NoError(t, r.RegisterAlias("alias-a", "a"))

	filtered := r.Filter(nil)
	assert.Equal(t, 2, filtered.Count())
	_, ok := filtered.Get("alias-a")
	assert.True(t, ok)
}
