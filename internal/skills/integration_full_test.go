package skills_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/skills/process"
	"github.com/julianshen/rubichan/internal/skills/starlark"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- helpers for full integration tests ---

// fullAutoApproveChecker is a PermissionChecker that approves everything.
type fullAutoApproveChecker struct{}

func (fullAutoApproveChecker) CheckPermission(_ skills.Permission) error { return nil }
func (fullAutoApproveChecker) CheckRateLimit(_ string) error             { return nil }
func (fullAutoApproveChecker) ResetTurnLimits()                          {}

// fullDenyChecker is a PermissionChecker that denies a specific permission.
type fullDenyChecker struct{ denied skills.Permission }

func (d fullDenyChecker) CheckPermission(p skills.Permission) error {
	if p == d.denied {
		return fmt.Errorf("permission %s denied", p)
	}
	return nil
}

func (fullDenyChecker) CheckRateLimit(_ string) error { return nil }
func (fullDenyChecker) ResetTurnLimits()              {}

// fullMockBackend is a mock backend for hook chain and permission tests.
type fullMockBackend struct {
	tools        []tools.Tool
	hooks        map[skills.HookPhase]skills.HookHandler
	loadCalled   bool
	unloadCalled bool
}

func (m *fullMockBackend) Load(_ skills.SkillManifest, _ skills.PermissionChecker) error {
	m.loadCalled = true
	return nil
}

func (m *fullMockBackend) Tools() []tools.Tool {
	return m.tools
}

func (m *fullMockBackend) Hooks() map[skills.HookPhase]skills.HookHandler {
	return m.hooks
}

func (m *fullMockBackend) Commands() []commands.SlashCommand { return nil }

func (m *fullMockBackend) Unload() error {
	m.unloadCalled = true
	return nil
}

// fullMockTool is a minimal tool for integration tests.
type fullMockTool struct {
	name        string
	description string
}

func (t *fullMockTool) Name() string                 { return t.name }
func (t *fullMockTool) Description() string          { return t.description }
func (t *fullMockTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t *fullMockTool) Execute(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{Content: "ok"}, nil
}

// echoSkillBinaryPath is built once per test run for TestFullLifecycleProcess.
var echoSkillBinaryPath string

func TestMain(m *testing.M) {
	// Build the echo_skill binary from the process testdata for process tests.
	tmpDir, err := os.MkdirTemp("", "integration-full-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	echoSkillBinaryPath = filepath.Join(tmpDir, "echo_skill")
	cmd := exec.Command("go", "build", "-o", echoSkillBinaryPath, "./process/testdata/echo_skill.go")
	cmd.Dir = filepath.Join(".", "")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to build echo_skill (process tests will be skipped): %v\n", err)
		echoSkillBinaryPath = ""
	}

	os.Exit(m.Run())
}

// TestFullLifecycleStarlark is an end-to-end test: create a temp Starlark skill
// on disk, use Loader to discover it, activate via Runtime, call a tool provided
// by the skill, then deactivate and verify cleanup.
func TestFullLifecycleStarlark(t *testing.T) {
	// 1. Create temp directory with SKILL.yaml and skill.star.
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "test-starlark")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))

	skillYAML := `name: test-starlark
version: "1.0.0"
description: "Integration test starlark skill"
types:
  - tool
implementation:
  backend: starlark
  entrypoint: skill.star
permissions:
  - file:read
`
	require.NoError(t, os.WriteFile(
		filepath.Join(skillDir, "SKILL.yaml"),
		[]byte(skillYAML),
		0o644,
	))

	// The Starlark skill registers a tool that echoes back input["msg"].
	skillStar := `
def echo_handler(input):
    return input["msg"]

register_tool(
    name = "echo-tool",
    description = "echoes input",
    handler = echo_handler,
)
`
	require.NoError(t, os.WriteFile(
		filepath.Join(skillDir, "skill.star"),
		[]byte(skillStar),
		0o644,
	))

	// 2. Create in-memory store.
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })

	// 3. Create Loader with temp dir as project skill directory.
	loader := skills.NewLoader("", tmpDir)

	// 4. Create tools registry.
	registry := tools.NewRegistry()

	// 5. Backend factory that routes to the real Starlark engine.
	backendFactory := func(m skills.SkillManifest, dir string) (skills.SkillBackend, error) {
		return starlark.NewEngine(m.Name, skillDir, &fullAutoApproveChecker{}), nil
	}

	// 6. Sandbox factory that auto-approves everything.
	sandboxFactory := func(name string, perms []skills.Permission) skills.PermissionChecker {
		return &fullAutoApproveChecker{}
	}

	// 7. Create Runtime with the skill auto-approved.
	rt := skills.NewRuntime(loader, s, registry, []string{"test-starlark"}, backendFactory, sandboxFactory)

	// 8. Discover skills from the project directory.
	err = rt.Discover(nil)
	require.NoError(t, err)

	// 9. Verify the skill was discovered (check it can be activated).
	// Since we can't access the private skills map, we verify through activation.
	err = rt.Activate("test-starlark")
	require.NoError(t, err)

	// 10. Verify skill is active.
	active := rt.GetActiveSkills()
	require.Len(t, active, 1)
	assert.Equal(t, "test-starlark", active[0].Manifest.Name)
	assert.Equal(t, skills.SkillStateActive, active[0].State)

	// 11. Get the "echo-tool" from the tools registry.
	echoTool, ok := registry.Get("echo-tool")
	require.True(t, ok, "echo-tool should be registered after activation")
	assert.Equal(t, "echo-tool", echoTool.Name())
	assert.Equal(t, "echoes input", echoTool.Description())

	// 12. Execute it with {"msg":"hello"}.
	input := json.RawMessage(`{"msg":"hello"}`)
	result, err := echoTool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "hello")

	// 13. Deactivate the skill.
	err = rt.Deactivate("test-starlark")
	require.NoError(t, err)

	// 14. Verify tool is unregistered.
	_, ok = registry.Get("echo-tool")
	assert.False(t, ok, "echo-tool should be unregistered after deactivation")

	// 15. Verify skill is not in the active list.
	assert.Empty(t, rt.GetActiveSkills())
}

// TestFullLifecycleProcess is an end-to-end test using the external process
// backend. It builds the echo_skill test binary, creates a skill on disk,
// discovers it, activates it, calls a tool, then deactivates.
func TestFullLifecycleProcess(t *testing.T) {
	if echoSkillBinaryPath == "" {
		t.Skip("echo_skill binary not available; skipping process integration test")
	}

	// 1. Create temp directory with SKILL.yaml pointing to the echo_skill binary.
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "test-process")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))

	skillYAML := fmt.Sprintf(`name: test-process
version: "1.0.0"
description: "Integration test process skill"
types:
  - tool
implementation:
  backend: process
  entrypoint: %s
permissions:
  - shell:exec
`, echoSkillBinaryPath)

	require.NoError(t, os.WriteFile(
		filepath.Join(skillDir, "SKILL.yaml"),
		[]byte(skillYAML),
		0o644,
	))

	// 2. Create in-memory store.
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })

	// 3. Create Loader with temp dir as project skill directory.
	loader := skills.NewLoader("", tmpDir)

	// 4. Create tools registry.
	registry := tools.NewRegistry()

	// 5. Backend factory that routes to the real process backend.
	// Use a generous timeout to handle slow CI environments and concurrent builds.
	backendFactory := func(m skills.SkillManifest, dir string) (skills.SkillBackend, error) {
		return process.NewProcessBackend(process.WithCallTimeout(30 * time.Second)), nil
	}

	// 6. Sandbox factory that auto-approves everything.
	sandboxFactory := func(name string, perms []skills.Permission) skills.PermissionChecker {
		return &fullAutoApproveChecker{}
	}

	// 7. Create Runtime with the skill auto-approved.
	rt := skills.NewRuntime(loader, s, registry, []string{"test-process"}, backendFactory, sandboxFactory)

	// 8. Discover skills from the project directory.
	err = rt.Discover(nil)
	require.NoError(t, err)

	// 9. Activate the skill.
	err = rt.Activate("test-process")
	require.NoError(t, err)

	// 10. Verify skill is active.
	active := rt.GetActiveSkills()
	require.Len(t, active, 1)
	assert.Equal(t, "test-process", active[0].Manifest.Name)
	assert.Equal(t, skills.SkillStateActive, active[0].State)

	// 11. The echo_skill declares an "echo" tool via initialize.
	echoTool, ok := registry.Get("echo")
	require.True(t, ok, "echo tool should be registered after activation")
	assert.Equal(t, "echo", echoTool.Name())

	// 12. Execute the tool.
	input := json.RawMessage(`{"message":"process-hello"}`)
	result, err := echoTool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "process-hello")

	// 13. Deactivate the skill.
	err = rt.Deactivate("test-process")
	require.NoError(t, err)

	// 14. Verify tool is unregistered.
	_, ok = registry.Get("echo")
	assert.False(t, ok, "echo tool should be unregistered after deactivation")

	// 15. Verify skill is not in the active list.
	assert.Empty(t, rt.GetActiveSkills())
}

// TestHookChainEndToEnd verifies that hooks from multiple skills execute in
// priority order (lower number first). It creates two skills at different
// priorities: one builtin (priority 0) and one from a project directory
// (priority 20). Both have OnBeforeToolCall hooks. After dispatching a tool
// call event, the test verifies that the builtin hook runs first.
func TestHookChainEndToEnd(t *testing.T) {
	// Track execution order via a shared slice.
	var executionOrder []string

	// Create two mock backends with OnBeforeToolCall hooks.
	hookBackendA := &fullMockBackend{
		tools: []tools.Tool{
			&fullMockTool{name: "skill-a-tool", description: "tool from skill-a"},
		},
		hooks: map[skills.HookPhase]skills.HookHandler{
			skills.HookOnBeforeToolCall: func(event skills.HookEvent) (skills.HookResult, error) {
				executionOrder = append(executionOrder, "skill-a")
				return skills.HookResult{}, nil
			},
		},
	}

	hookBackendB := &fullMockBackend{
		tools: []tools.Tool{
			&fullMockTool{name: "skill-b-tool", description: "tool from skill-b"},
		},
		hooks: map[skills.HookPhase]skills.HookHandler{
			skills.HookOnBeforeToolCall: func(event skills.HookEvent) (skills.HookResult, error) {
				executionOrder = append(executionOrder, "skill-b")
				return skills.HookResult{}, nil
			},
		},
	}

	// Backend factory returns the appropriate backend for each skill.
	backendFactory := func(m skills.SkillManifest, dir string) (skills.SkillBackend, error) {
		switch m.Name {
		case "skill-a":
			return hookBackendA, nil
		case "skill-b":
			return hookBackendB, nil
		default:
			return nil, fmt.Errorf("unknown skill %q", m.Name)
		}
	}

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })

	registry := tools.NewRegistry()
	sandboxFactory := func(name string, perms []skills.Permission) skills.PermissionChecker {
		return &fullAutoApproveChecker{}
	}

	// Create a temp project directory with skill-b so it gets SourceProject priority.
	tmpDir := t.TempDir()
	skillBDir := filepath.Join(tmpDir, "skill-b")
	require.NoError(t, os.MkdirAll(skillBDir, 0o755))

	skillBYAML := `name: skill-b
version: "1.0.0"
description: "Skill B with project priority"
types:
  - tool
implementation:
  backend: starlark
  entrypoint: main.star
`
	require.NoError(t, os.WriteFile(
		filepath.Join(skillBDir, "SKILL.yaml"),
		[]byte(skillBYAML),
		0o644,
	))

	// Create loader with the project dir containing skill-b.
	loader := skills.NewLoader("", tmpDir)

	// Register skill-a as builtin (SourceBuiltin -> priority 0).
	manifestA := &skills.SkillManifest{
		Name:        "skill-a",
		Version:     "1.0.0",
		Description: "Skill A with builtin priority",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	loader.RegisterBuiltin(manifestA)

	rt := skills.NewRuntime(loader, s, registry, []string{"skill-a", "skill-b"}, backendFactory, sandboxFactory)

	// Discover: skill-a comes from builtin (priority 0), skill-b from project dir (priority 20).
	require.NoError(t, rt.Discover(nil))

	// Activate both skills.
	require.NoError(t, rt.Activate("skill-a"))
	require.NoError(t, rt.Activate("skill-b"))

	// Verify both are active.
	active := rt.GetActiveSkills()
	require.Len(t, active, 2)

	// Dispatch a OnBeforeToolCall event.
	_, err = rt.DispatchHook(skills.HookEvent{
		Phase: skills.HookOnBeforeToolCall,
		Data:  map[string]any{"tool_name": "test-tool"},
		Ctx:   context.Background(),
	})
	require.NoError(t, err)

	// Verify hooks execute in priority order: skill-a (builtin, 0) before skill-b (project, 20).
	require.Len(t, executionOrder, 2, "both hooks should have executed")
	assert.Equal(t, "skill-a", executionOrder[0], "builtin hook (priority 0) should execute first")
	assert.Equal(t, "skill-b", executionOrder[1], "project hook (priority 20) should execute second")
}

// TestPermissionDenialEndToEnd verifies that a skill requiring "shell:exec"
// permission fails to activate when the sandbox denies that permission. It
// confirms the skill does not appear in the active list and that the error
// message is appropriate.
func TestPermissionDenialEndToEnd(t *testing.T) {
	backendFactory := func(m skills.SkillManifest, dir string) (skills.SkillBackend, error) {
		return &fullMockBackend{
			tools: []tools.Tool{
				&fullMockTool{name: m.Name + "-tool", description: "tool from " + m.Name},
			},
			hooks: map[skills.HookPhase]skills.HookHandler{},
		}, nil
	}

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })

	registry := tools.NewRegistry()

	// Sandbox factory that denies shell:exec.
	sandboxFactory := func(name string, perms []skills.Permission) skills.PermissionChecker {
		return fullDenyChecker{denied: skills.PermShellExec}
	}

	// Do NOT auto-approve the skill so sandbox checks run.
	loader := skills.NewLoader("", "")
	rt := skills.NewRuntime(loader, s, registry, nil, backendFactory, sandboxFactory)

	m := &skills.SkillManifest{
		Name:        "restricted-skill",
		Version:     "1.0.0",
		Description: "A skill requiring shell:exec",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
		Permissions: []skills.Permission{skills.PermShellExec},
	}
	loader.RegisterBuiltin(m)
	require.NoError(t, rt.Discover(nil))

	// Attempt to activate the skill.
	err = rt.Activate("restricted-skill")
	require.Error(t, err, "activation should fail when shell:exec is denied")
	assert.Contains(t, err.Error(), "permission")
	assert.Contains(t, err.Error(), "denied")

	// Verify the skill is NOT in the active list.
	active := rt.GetActiveSkills()
	assert.Empty(t, active, "restricted-skill should not be active")

	// Verify the tool was NOT registered.
	_, ok := registry.Get("restricted-skill-tool")
	assert.False(t, ok, "tool should not be registered when activation fails")
}
