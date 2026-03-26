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

func TestRegistryFilter(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(newMockTool("file", "file tool")))
	require.NoError(t, r.Register(newMockTool("shell", "shell tool")))
	require.NoError(t, r.Register(newMockTool("search", "search tool")))

	filtered := r.Filter([]string{"file", "search"})
	_, ok := filtered.Get("file")
	assert.True(t, ok)
	_, ok = filtered.Get("search")
	assert.True(t, ok)
	_, ok = filtered.Get("shell")
	assert.False(t, ok)
	assert.Len(t, filtered.All(), 2)
}

func TestRegistryFilterNilReturnsAll(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(newMockTool("file", "file tool")))
	require.NoError(t, r.Register(newMockTool("shell", "shell tool")))
	filtered := r.Filter(nil)
	assert.Len(t, filtered.All(), 2)
}

func TestRegistryFilterUnknownNamesIgnored(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(newMockTool("file", "file tool")))
	filtered := r.Filter([]string{"file", "nonexistent"})
	assert.Len(t, filtered.All(), 1)
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

func TestRegistryAlias(t *testing.T) {
	reg := NewRegistry()
	tool := newMockTool("shell", "Execute shell commands")
	require.NoError(t, reg.Register(tool))

	// Register alias.
	require.NoError(t, reg.RegisterAlias("shell_exec", "shell"))
	require.NoError(t, reg.RegisterAlias("run_command", "shell"))

	// Lookup by canonical name still works.
	got, ok := reg.Get("shell")
	assert.True(t, ok)
	assert.Equal(t, "shell", got.Name())

	// Lookup by alias resolves to the real tool.
	got, ok = reg.Get("shell_exec")
	assert.True(t, ok)
	assert.Equal(t, "shell", got.Name())

	got, ok = reg.Get("run_command")
	assert.True(t, ok)
	assert.Equal(t, "shell", got.Name())

	// Alias to nonexistent tool returns not-found.
	require.NoError(t, reg.RegisterAlias("bad_alias", "nonexistent"))
	_, ok = reg.Get("bad_alias")
	assert.False(t, ok)
}

func TestRegistryAliasShadowCanonicalReturnsError(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register(newMockTool("shell", "shell")))

	err := reg.RegisterAlias("shell", "file")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "shadows canonical tool name")
}

func TestRegistryAliasDuplicateReturnsError(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.RegisterAlias("my_alias", "shell"))

	// Same target is fine (idempotent).
	require.NoError(t, reg.RegisterAlias("my_alias", "shell"))

	// Different target is an error.
	err := reg.RegisterAlias("my_alias", "file")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestRegistryAliasDoesNotAppearInAll(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register(newMockTool("file", "File operations")))
	require.NoError(t, reg.RegisterAlias("write_file", "file"))
	require.NoError(t, reg.RegisterAlias("read_file", "file"))

	// All() should only return canonical tools, not aliases.
	defs := reg.All()
	assert.Len(t, defs, 1)
	assert.Equal(t, "file", defs[0].Name)
}

func TestRegistryFilterCopiesAliases(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register(newMockTool("shell", "shell")))
	require.NoError(t, reg.Register(newMockTool("file", "file")))
	require.NoError(t, reg.RegisterAlias("bash", "shell"))
	require.NoError(t, reg.RegisterAlias("write_file", "file"))

	// Filter to only "shell" — alias "bash" should come along, "write_file" should not.
	filtered := reg.Filter([]string{"shell"})
	got, ok := filtered.Get("bash")
	assert.True(t, ok, "alias 'bash' should be copied to filtered registry")
	assert.Equal(t, "shell", got.Name())

	_, ok = filtered.Get("write_file")
	assert.False(t, ok, "alias 'write_file' should not be in filtered registry")

	// Filter nil copies all aliases.
	all := reg.Filter(nil)
	_, ok = all.Get("bash")
	assert.True(t, ok)
	_, ok = all.Get("write_file")
	assert.True(t, ok)
}

func TestRegistryDefaultAliases(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register(newMockTool("shell", "shell")))
	require.NoError(t, reg.Register(newMockTool("file", "file")))
	require.NoError(t, reg.Register(newMockTool("search", "search")))
	require.NoError(t, reg.Register(newMockTool("process", "process")))

	reg.RegisterDefaultAliases()

	// Shell aliases resolve.
	for _, alias := range []string{"shell_exec", "run_command", "bash", "exec"} {
		got, ok := reg.Get(alias)
		assert.True(t, ok, "alias %q should resolve", alias)
		assert.Equal(t, "shell", got.Name())
	}

	// File aliases resolve.
	for _, alias := range []string{"write_file", "read_file", "file_write", "edit_file"} {
		got, ok := reg.Get(alias)
		assert.True(t, ok, "alias %q should resolve", alias)
		assert.Equal(t, "file", got.Name())
	}

	// Search aliases resolve.
	got, ok := reg.Get("grep")
	assert.True(t, ok)
	assert.Equal(t, "search", got.Name())

	// All() still returns only canonical tools.
	assert.Len(t, reg.All(), 4)
}

func TestRegistryNames(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register(newMockTool("shell", "shell")))
	require.NoError(t, reg.Register(newMockTool("file", "file")))

	names := reg.Names()
	assert.Len(t, names, 2)
	assert.Contains(t, names, "shell")
	assert.Contains(t, names, "file")
}
