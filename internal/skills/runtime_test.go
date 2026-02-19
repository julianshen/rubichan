package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mock backend for testing ---

type mockBackend struct {
	tools        []tools.Tool
	hooks        map[HookPhase]HookHandler
	loadCalled   bool
	unloadCalled bool
	loadErr      error
}

func (m *mockBackend) Load(manifest SkillManifest, sb PermissionChecker) error {
	m.loadCalled = true
	return m.loadErr
}

func (m *mockBackend) Tools() []tools.Tool {
	return m.tools
}

func (m *mockBackend) Hooks() map[HookPhase]HookHandler {
	return m.hooks
}

func (m *mockBackend) Unload() error {
	m.unloadCalled = true
	return nil
}

// --- mock tool for testing ---

type runtimeMockTool struct {
	name        string
	description string
}

func (t *runtimeMockTool) Name() string                 { return t.name }
func (t *runtimeMockTool) Description() string          { return t.description }
func (t *runtimeMockTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t *runtimeMockTool) Execute(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{Content: "ok"}, nil
}

// --- mock permission checker ---

type mockPermissionChecker struct {
	denyPerms map[Permission]bool
}

func (m *mockPermissionChecker) CheckPermission(perm Permission) error {
	if m.denyPerms[perm] {
		return fmt.Errorf("permission %q not approved", perm)
	}
	return nil
}

func (m *mockPermissionChecker) CheckRateLimit(_ string) error { return nil }
func (m *mockPermissionChecker) ResetTurnLimits()              {}

// --- test helpers ---

// newTestRuntime creates a Runtime wired with in-memory SQLite and a mock backend factory.
// It returns the runtime, the store, and a pointer to the last mockBackend created by the factory.
func newTestRuntime(t *testing.T, autoApprove []string, denyPerms map[Permission]bool) (*Runtime, *store.Store, **mockBackend) {
	t.Helper()

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })

	registry := tools.NewRegistry()

	var lastBackend *mockBackend
	backendFactory := func(manifest SkillManifest, dir string) (SkillBackend, error) {
		mb := &mockBackend{
			tools: []tools.Tool{
				&runtimeMockTool{
					name:        manifest.Name + "-tool",
					description: "Tool from " + manifest.Name,
				},
			},
			hooks: map[HookPhase]HookHandler{
				HookOnActivate: func(event HookEvent) (HookResult, error) {
					return HookResult{}, nil
				},
			},
		}
		lastBackend = mb
		return mb, nil
	}

	checker := &mockPermissionChecker{denyPerms: denyPerms}
	sandboxFactory := func(skillName string, declared []Permission) PermissionChecker {
		return checker
	}

	rt := NewRuntime(NewLoader("", ""), s, registry, autoApprove, backendFactory, sandboxFactory)
	return rt, s, &lastBackend
}

// testManifest returns a minimal valid SkillManifest for testing.
func testManifest(name string) *SkillManifest {
	return &SkillManifest{
		Name:        name,
		Version:     "1.0.0",
		Description: "Test skill " + name,
		Types:       []SkillType{SkillTypeTool},
		Implementation: ImplementationConfig{
			Backend:    BackendStarlark,
			Entrypoint: "main.star",
		},
		Permissions: []Permission{PermFileRead},
	}
}

func TestRuntimeDiscoverAndActivate(t *testing.T) {
	rt, _, lastBackend := newTestRuntime(t, []string{"discover-skill"}, nil)

	// Register a builtin skill.
	m := testManifest("discover-skill")
	rt.loader.RegisterBuiltin(m)

	// Discover should populate the skills map.
	err := rt.Discover(nil)
	require.NoError(t, err)
	assert.Contains(t, rt.skills, "discover-skill")
	assert.Equal(t, SkillStateInactive, rt.skills["discover-skill"].State)

	// Activate the skill.
	err = rt.Activate("discover-skill")
	require.NoError(t, err)

	// Verify backend was loaded and skill is active.
	assert.True(t, (*lastBackend).loadCalled, "backend Load should have been called")
	assert.Equal(t, SkillStateActive, rt.skills["discover-skill"].State)
	assert.Contains(t, rt.active, "discover-skill")

	// Verify the tool was registered.
	tool, ok := rt.registry.Get("discover-skill-tool")
	assert.True(t, ok, "skill tool should be registered in the registry")
	assert.Equal(t, "discover-skill-tool", tool.Name())
}

func TestRuntimeTriggerActivation(t *testing.T) {
	rt, _, _ := newTestRuntime(t, []string{"triggered-skill"}, nil)

	// Register a skill with file triggers.
	m := testManifest("triggered-skill")
	m.Triggers = TriggerConfig{
		Files: []string{"go.mod"},
	}
	rt.loader.RegisterBuiltin(m)

	// Discover first.
	err := rt.Discover(nil)
	require.NoError(t, err)

	// EvaluateAndActivate with matching trigger context.
	ctx := TriggerContext{
		ProjectFiles: []string{"go.mod", "main.go"},
	}
	err = rt.EvaluateAndActivate(ctx)
	require.NoError(t, err)

	// Skill should now be active.
	assert.Equal(t, SkillStateActive, rt.skills["triggered-skill"].State)
	assert.Contains(t, rt.active, "triggered-skill")
}

func TestRuntimePermissionDenied(t *testing.T) {
	// Create a runtime where file:read is denied.
	denied := map[Permission]bool{PermFileRead: true}
	rt, _, _ := newTestRuntime(t, nil, denied)

	// Register a skill that requires permissions.
	m := testManifest("denied-skill")
	m.Permissions = []Permission{PermFileRead}
	rt.loader.RegisterBuiltin(m)

	// Discover.
	err := rt.Discover(nil)
	require.NoError(t, err)

	// Activate should fail because permissions are not approved.
	err = rt.Activate("denied-skill")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not approved")

	// Skill should not be active.
	assert.NotEqual(t, SkillStateActive, rt.skills["denied-skill"].State)
	assert.NotContains(t, rt.active, "denied-skill")
}

func TestRuntimeDeactivate(t *testing.T) {
	rt, _, lastBackend := newTestRuntime(t, []string{"deactivate-skill"}, nil)

	m := testManifest("deactivate-skill")
	rt.loader.RegisterBuiltin(m)

	// Discover and activate.
	require.NoError(t, rt.Discover(nil))
	require.NoError(t, rt.Activate("deactivate-skill"))
	assert.Equal(t, SkillStateActive, rt.skills["deactivate-skill"].State)

	// Deactivate.
	err := rt.Deactivate("deactivate-skill")
	require.NoError(t, err)

	// Verify backend was unloaded.
	assert.True(t, (*lastBackend).unloadCalled, "backend Unload should have been called")

	// Verify skill is inactive.
	assert.Equal(t, SkillStateInactive, rt.skills["deactivate-skill"].State)
	assert.NotContains(t, rt.active, "deactivate-skill")

	// Verify tool was unregistered.
	_, ok := rt.registry.Get("deactivate-skill-tool")
	assert.False(t, ok, "skill tool should be removed from registry after deactivation")
}

func TestRuntimeGetActiveSkills(t *testing.T) {
	rt, _, _ := newTestRuntime(t, []string{"active-one", "active-two", "inactive-one"}, nil)

	// Register three skills.
	for _, name := range []string{"active-one", "active-two", "inactive-one"} {
		m := testManifest(name)
		rt.loader.RegisterBuiltin(m)
	}

	require.NoError(t, rt.Discover(nil))

	// Activate only two of them.
	require.NoError(t, rt.Activate("active-one"))
	require.NoError(t, rt.Activate("active-two"))

	active := rt.GetActiveSkills()
	assert.Len(t, active, 2)

	names := make(map[string]bool)
	for _, sk := range active {
		names[sk.Manifest.Name] = true
		assert.Equal(t, SkillStateActive, sk.State)
	}
	assert.True(t, names["active-one"])
	assert.True(t, names["active-two"])
	assert.False(t, names["inactive-one"])
}

func TestRuntimeToolRegistration(t *testing.T) {
	rt, _, _ := newTestRuntime(t, []string{"tool-reg-skill"}, nil)

	m := testManifest("tool-reg-skill")
	rt.loader.RegisterBuiltin(m)

	require.NoError(t, rt.Discover(nil))

	// Before activation, tool should not exist.
	_, ok := rt.registry.Get("tool-reg-skill-tool")
	assert.False(t, ok, "tool should not exist before activation")

	// Activate.
	require.NoError(t, rt.Activate("tool-reg-skill"))

	// After activation, tool should exist.
	tool, ok := rt.registry.Get("tool-reg-skill-tool")
	require.True(t, ok, "tool should exist after activation")
	assert.Equal(t, "tool-reg-skill-tool", tool.Name())

	// Verify tool can be found in All().
	allDefs := rt.registry.All()
	found := false
	for _, def := range allDefs {
		if def.Name == "tool-reg-skill-tool" {
			found = true
			break
		}
	}
	assert.True(t, found, "tool-reg-skill-tool should appear in registry.All()")

	// Deactivate - tool should be removed.
	require.NoError(t, rt.Deactivate("tool-reg-skill"))
	_, ok = rt.registry.Get("tool-reg-skill-tool")
	assert.False(t, ok, "tool should be removed after deactivation")
}

func TestRuntimeActivateNotFound(t *testing.T) {
	rt, _, _ := newTestRuntime(t, nil, nil)

	err := rt.Activate("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRuntimeDeactivateNotActive(t *testing.T) {
	rt, _, _ := newTestRuntime(t, nil, nil)

	err := rt.Deactivate("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not active")
}

func TestRuntimeActivateBackendFactoryError(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	registry := tools.NewRegistry()
	factoryErr := fmt.Errorf("factory exploded")

	backendFactory := func(manifest SkillManifest, dir string) (SkillBackend, error) {
		return nil, factoryErr
	}
	sandboxFactory := func(skillName string, declared []Permission) PermissionChecker {
		return &mockPermissionChecker{}
	}

	rt := NewRuntime(NewLoader("", ""), s, registry, nil, backendFactory, sandboxFactory)

	m := testManifest("factory-fail")
	rt.loader.RegisterBuiltin(m)
	require.NoError(t, rt.Discover(nil))

	err = rt.Activate("factory-fail")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "factory exploded")
	// Skill should be back to Inactive after error.
	assert.Equal(t, SkillStateInactive, rt.skills["factory-fail"].State)
}

func TestRuntimeActivateBackendLoadError(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	registry := tools.NewRegistry()

	backendFactory := func(manifest SkillManifest, dir string) (SkillBackend, error) {
		return &mockBackend{
			loadErr: fmt.Errorf("load failed"),
		}, nil
	}
	sandboxFactory := func(skillName string, declared []Permission) PermissionChecker {
		return &mockPermissionChecker{}
	}

	rt := NewRuntime(NewLoader("", ""), s, registry, nil, backendFactory, sandboxFactory)

	m := testManifest("load-fail")
	rt.loader.RegisterBuiltin(m)
	require.NoError(t, rt.Discover(nil))

	err = rt.Activate("load-fail")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load failed")
	assert.Equal(t, SkillStateInactive, rt.skills["load-fail"].State)
}

func TestRuntimeSourcePriority(t *testing.T) {
	// Verify all source priority paths.
	assert.Equal(t, PriorityBuiltin, sourcePriority(SourceBuiltin))
	assert.Equal(t, PriorityUser, sourcePriority(SourceUser))
	assert.Equal(t, PriorityUser, sourcePriority(SourceInline))
	assert.Equal(t, PriorityProject, sourcePriority(SourceProject))
	assert.Equal(t, PriorityProject, sourcePriority(Source("unknown")))
}

func TestRuntimeEvaluateAndActivateSkipsAlreadyActive(t *testing.T) {
	rt, _, _ := newTestRuntime(t, []string{"already-active"}, nil)

	m := testManifest("already-active")
	m.Triggers = TriggerConfig{Files: []string{"go.mod"}}
	rt.loader.RegisterBuiltin(m)

	require.NoError(t, rt.Discover(nil))
	require.NoError(t, rt.Activate("already-active"))

	// EvaluateAndActivate again should not error or double-activate.
	ctx := TriggerContext{ProjectFiles: []string{"go.mod"}}
	err := rt.EvaluateAndActivate(ctx)
	require.NoError(t, err)

	// Should still be active (only once).
	assert.Len(t, rt.active, 1)
}

func TestRuntimeEvaluateAndActivatePermissionError(t *testing.T) {
	denied := map[Permission]bool{PermFileRead: true}
	rt, _, _ := newTestRuntime(t, nil, denied)

	m := testManifest("perm-fail")
	m.Triggers = TriggerConfig{Files: []string{"go.mod"}}
	rt.loader.RegisterBuiltin(m)

	require.NoError(t, rt.Discover(nil))

	ctx := TriggerContext{ProjectFiles: []string{"go.mod"}}
	err := rt.EvaluateAndActivate(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not approved")
}

func TestRuntimeGetActiveSkillsEmpty(t *testing.T) {
	rt, _, _ := newTestRuntime(t, nil, nil)
	active := rt.GetActiveSkills()
	assert.Empty(t, active)
}

func TestRuntimeDiscoverError(t *testing.T) {
	rt, _, _ := newTestRuntime(t, nil, nil)

	// Request an explicit skill that doesn't exist. This causes Discover to fail.
	err := rt.Discover([]string{"nonexistent-skill"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "discover skills")
}

func TestRuntimeActivateToolRegistrationError(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	registry := tools.NewRegistry()

	// Pre-register a tool with the same name the backend will try to register.
	preExisting := &runtimeMockTool{name: "dup-skill-tool", description: "pre-existing"}
	require.NoError(t, registry.Register(preExisting))

	backendFactory := func(manifest SkillManifest, dir string) (SkillBackend, error) {
		return &mockBackend{
			tools: []tools.Tool{
				&runtimeMockTool{name: manifest.Name + "-tool", description: "from backend"},
			},
			hooks: map[HookPhase]HookHandler{},
		}, nil
	}
	sandboxFactory := func(skillName string, declared []Permission) PermissionChecker {
		return &mockPermissionChecker{}
	}

	rt := NewRuntime(NewLoader("", ""), s, registry, nil, backendFactory, sandboxFactory)

	m := testManifest("dup-skill")
	rt.loader.RegisterBuiltin(m)
	require.NoError(t, rt.Discover(nil))

	err = rt.Activate("dup-skill")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "register tool")
	// Skill should be back to Inactive.
	assert.Equal(t, SkillStateInactive, rt.skills["dup-skill"].State)
}

func TestRuntimeDeactivateNilBackend(t *testing.T) {
	rt, _, _ := newTestRuntime(t, []string{"nil-backend"}, nil)

	m := testManifest("nil-backend")
	rt.loader.RegisterBuiltin(m)
	require.NoError(t, rt.Discover(nil))

	// Manually force the skill to Active with a nil backend to test nil-guard paths.
	sk := rt.skills["nil-backend"]
	sk.State = SkillStateActive
	sk.Backend = nil
	rt.active["nil-backend"] = sk

	err := rt.Deactivate("nil-backend")
	require.NoError(t, err)
	assert.Equal(t, SkillStateInactive, sk.State)
	assert.NotContains(t, rt.active, "nil-backend")
}
