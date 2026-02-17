package starlark_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/skills/starlark"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockChecker is a no-op PermissionChecker for testing.
type mockChecker struct{}

func (m *mockChecker) CheckPermission(_ skills.Permission) error { return nil }
func (m *mockChecker) CheckRateLimit(_ string) error             { return nil }
func (m *mockChecker) ResetTurnLimits()                          {}

// writeStarFile writes a .star file into the given directory and returns
// a SkillManifest that points to it.
func writeStarFile(t *testing.T, dir, filename, code string) skills.SkillManifest {
	t.Helper()
	err := os.WriteFile(filepath.Join(dir, filename), []byte(code), 0o644)
	require.NoError(t, err)

	return skills.SkillManifest{
		Name:        "test-skill",
		Version:     "0.1.0",
		Description: "test skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: filename,
		},
	}
}

func TestEngineExecSimple(t *testing.T) {
	dir := t.TempDir()
	manifest := writeStarFile(t, dir, "main.star", `
x = 1 + 2
`)

	engine := starlark.NewEngine("test-skill", dir, &mockChecker{})
	err := engine.Load(manifest, &mockChecker{})
	require.NoError(t, err)

	// Simple execution completes without error; no tools registered.
	assert.Empty(t, engine.Tools())
	assert.Empty(t, engine.Hooks())

	err = engine.Unload()
	assert.NoError(t, err)
}

func TestEngineExecError(t *testing.T) {
	dir := t.TempDir()
	manifest := writeStarFile(t, dir, "main.star", `
this is not valid starlark !!!
`)

	engine := starlark.NewEngine("test-skill", dir, &mockChecker{})
	err := engine.Load(manifest, &mockChecker{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "starlark")
}

func TestEngineExecBuiltinAvailable(t *testing.T) {
	dir := t.TempDir()
	// The log() builtin should be available in scope. If it is not, execution
	// will fail with an "undefined: log" error.
	manifest := writeStarFile(t, dir, "main.star", `
log("hello from starlark")
`)

	engine := starlark.NewEngine("test-skill", dir, &mockChecker{})
	err := engine.Load(manifest, &mockChecker{})
	require.NoError(t, err)

	err = engine.Unload()
	assert.NoError(t, err)
}

func TestEngineExecRegisterTool(t *testing.T) {
	dir := t.TempDir()
	manifest := writeStarFile(t, dir, "main.star", `
def greet(input):
    name = input.get("name", "world")
    return "Hello, " + name + "!"

register_tool(
    name = "greet",
    description = "Greets a person by name",
    handler = greet,
)
`)

	engine := starlark.NewEngine("test-skill", dir, &mockChecker{})
	err := engine.Load(manifest, &mockChecker{})
	require.NoError(t, err)

	// Verify the tool was registered.
	registeredTools := engine.Tools()
	require.Len(t, registeredTools, 1)

	tool := registeredTools[0]
	assert.Equal(t, "greet", tool.Name())
	assert.Equal(t, "Greets a person by name", tool.Description())

	// InputSchema should be valid JSON.
	schema := tool.InputSchema()
	assert.True(t, json.Valid(schema), "InputSchema() should return valid JSON")

	// Execute the tool with input.
	inputJSON, err := json.Marshal(map[string]any{"name": "Alice"})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), inputJSON)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, "Hello, Alice!", result.Content)

	// Execute with no name should use default.
	result2, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "Hello, world!", result2.Content)

	err = engine.Unload()
	assert.NoError(t, err)
}

func TestEngineExecRegisterHook(t *testing.T) {
	dir := t.TempDir()
	// register_hook is a placeholder that accepts any args without error.
	manifest := writeStarFile(t, dir, "main.star", `
register_hook()
`)

	engine := starlark.NewEngine("test-skill", dir, &mockChecker{})
	err := engine.Load(manifest, &mockChecker{})
	require.NoError(t, err)

	// Hooks map is empty because register_hook is a placeholder.
	assert.Empty(t, engine.Hooks())

	err = engine.Unload()
	assert.NoError(t, err)
}

func TestEngineLoadDefaultEntrypoint(t *testing.T) {
	dir := t.TempDir()

	// Write main.star (the default entrypoint).
	err := os.WriteFile(filepath.Join(dir, "main.star"), []byte(`x = 42`), 0o644)
	require.NoError(t, err)

	manifest := skills.SkillManifest{
		Name:        "test-default",
		Version:     "0.1.0",
		Description: "test default entrypoint",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "", // empty triggers default
		},
	}

	engine := starlark.NewEngine("test-default", dir, &mockChecker{})
	err = engine.Load(manifest, &mockChecker{})
	require.NoError(t, err)

	err = engine.Unload()
	assert.NoError(t, err)
}

func TestEngineLoadMissingFile(t *testing.T) {
	dir := t.TempDir()

	manifest := skills.SkillManifest{
		Name:        "test-missing",
		Version:     "0.1.0",
		Description: "test missing file",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "nonexistent.star",
		},
	}

	engine := starlark.NewEngine("test-missing", dir, &mockChecker{})
	err := engine.Load(manifest, &mockChecker{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read starlark entrypoint")
}

func TestEngineToolExecuteInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	manifest := writeStarFile(t, dir, "main.star", `
def echo(input):
    return "ok"

register_tool(name="echo", description="echo", handler=echo)
`)

	engine := starlark.NewEngine("test-skill", dir, &mockChecker{})
	err := engine.Load(manifest, &mockChecker{})
	require.NoError(t, err)

	tool := engine.Tools()[0]

	// Invalid JSON input should return an error.
	result, err := tool.Execute(context.Background(), json.RawMessage(`{invalid`))
	require.Error(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, err.Error(), "unmarshal input")
}

func TestEngineToolExecuteHandlerError(t *testing.T) {
	dir := t.TempDir()
	manifest := writeStarFile(t, dir, "main.star", `
def fail(input):
    return 1 / 0  # division by zero

register_tool(name="fail", description="always fails", handler=fail)
`)

	engine := starlark.NewEngine("test-skill", dir, &mockChecker{})
	err := engine.Load(manifest, &mockChecker{})
	require.NoError(t, err)

	tool := engine.Tools()[0]

	result, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	require.Error(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, err.Error(), "call starlark handler")
}

func TestEngineToolExecuteNonStringReturn(t *testing.T) {
	dir := t.TempDir()
	manifest := writeStarFile(t, dir, "main.star", `
def number(input):
    return 42

register_tool(name="number", description="returns a number", handler=number)
`)

	engine := starlark.NewEngine("test-skill", dir, &mockChecker{})
	err := engine.Load(manifest, &mockChecker{})
	require.NoError(t, err)

	tool := engine.Tools()[0]

	result, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, "42", result.Content)
}

func TestEngineToolExecuteComplexInput(t *testing.T) {
	dir := t.TempDir()
	// Test various input types: string, number, bool, null, nested dict, list.
	manifest := writeStarFile(t, dir, "main.star", `
def inspect(input):
    parts = []
    name = input.get("name", "")
    if name:
        parts.append("name=" + name)
    count = input.get("count", 0)
    if count:
        parts.append("count=" + str(count))
    flag = input.get("flag", False)
    if flag:
        parts.append("flag=true")
    items = input.get("items", [])
    if items:
        parts.append("items=" + str(len(items)))
    nested = input.get("nested", None)
    if nested:
        parts.append("nested=yes")
    nothing = input.get("nothing", "default")
    if nothing == None:
        parts.append("nothing=null")
    return ",".join(parts)

register_tool(name="inspect", description="inspects input", handler=inspect)
`)

	engine := starlark.NewEngine("test-skill", dir, &mockChecker{})
	err := engine.Load(manifest, &mockChecker{})
	require.NoError(t, err)

	tool := engine.Tools()[0]

	input := map[string]any{
		"name":    "test",
		"count":   3.0,
		"flag":    true,
		"items":   []any{"a", "b"},
		"nested":  map[string]any{"key": "val"},
		"nothing": nil,
	}
	inputJSON, err := json.Marshal(input)
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), inputJSON)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "name=test")
	assert.Contains(t, result.Content, "count=3")
	assert.Contains(t, result.Content, "flag=true")
	assert.Contains(t, result.Content, "items=2")
	assert.Contains(t, result.Content, "nested=yes")
	assert.Contains(t, result.Content, "nothing=null")
}

func TestEngineToolExecuteEmptyInput(t *testing.T) {
	dir := t.TempDir()
	manifest := writeStarFile(t, dir, "main.star", `
def noop(input):
    return "done"

register_tool(name="noop", description="noop", handler=noop)
`)

	engine := starlark.NewEngine("test-skill", dir, &mockChecker{})
	err := engine.Load(manifest, &mockChecker{})
	require.NoError(t, err)

	tool := engine.Tools()[0]

	// Empty input (nil RawMessage).
	result, err := tool.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, "done", result.Content)
}

func TestEngineToolExecuteFloatInput(t *testing.T) {
	dir := t.TempDir()
	manifest := writeStarFile(t, dir, "main.star", `
def show_pi(input):
    pi = input.get("pi", 0)
    return str(pi)

register_tool(name="show-pi", description="shows pi", handler=show_pi)
`)

	engine := starlark.NewEngine("test-skill", dir, &mockChecker{})
	err := engine.Load(manifest, &mockChecker{})
	require.NoError(t, err)

	tool := engine.Tools()[0]

	// 3.14 is a genuine float (not an integer).
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"pi": 3.14}`))
	require.NoError(t, err)
	assert.Equal(t, "3.14", result.Content)
}
