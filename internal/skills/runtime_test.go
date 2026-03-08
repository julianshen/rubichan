package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/commands"
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

func (m *mockBackend) Commands() []commands.SlashCommand { return nil }

func (m *mockBackend) Agents() []*AgentDefinition { return nil }

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

func TestRuntimeDiscoverStoresWarnings(t *testing.T) {
	userDir := t.TempDir()
	skillDir := filepath.Join(userDir, "opt-dep-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.yaml"), []byte(`name: opt-dep-skill
version: 1.0.0
description: "skill with optional dependency"
types:
  - tool
implementation:
  backend: starlark
  entrypoint: skill.star
dependencies:
  - name: missing-optional
    optional: true
`), 0o644))

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	rt := NewRuntime(NewLoader(userDir, ""), s, tools.NewRegistry(), nil,
		func(manifest SkillManifest, dir string) (SkillBackend, error) { return &mockBackend{}, nil },
		func(skillName string, declared []Permission) PermissionChecker { return &mockPermissionChecker{} },
	)

	require.NoError(t, rt.Discover(nil))
	warnings := rt.GetDiscoveryWarnings()
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "missing-optional")
	assert.Contains(t, warnings[0], "opt-dep-skill")
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

func TestRuntimeEvaluateAndActivateUsesActivationThreshold(t *testing.T) {
	rt, _, _ := newTestRuntime(t, nil, nil)
	rt.SetActivationThreshold(100)

	m := testManifest("go-skill")
	m.Triggers = TriggerConfig{Languages: []string{"go"}}
	rt.loader.RegisterBuiltin(m)

	require.NoError(t, rt.Discover(nil))
	err := rt.EvaluateAndActivate(TriggerContext{DetectedLangs: []string{"go"}})
	require.NoError(t, err)

	assert.NotContains(t, rt.active, "go-skill")
	report, ok := rt.GetActivationReport("go-skill")
	require.True(t, ok)
	assert.False(t, report.Activated)
	assert.Equal(t, 60, report.Score.Languages)
}

func TestRuntimeGetActivationReports(t *testing.T) {
	rt, _, _ := newTestRuntime(t, nil, nil)

	fileSkill := testManifest("file-skill")
	fileSkill.Triggers = TriggerConfig{Files: []string{"go.mod"}}
	rt.loader.RegisterBuiltin(fileSkill)

	explicitSkill := testManifest("explicit-skill")
	rt.loader.RegisterBuiltin(explicitSkill)

	require.NoError(t, rt.Discover([]string{"explicit-skill"}))
	err := rt.EvaluateAndActivate(TriggerContext{ProjectFiles: []string{"go.mod"}})
	require.NoError(t, err)

	reports := rt.GetActivationReports()
	require.Len(t, reports, 2)
	assert.Equal(t, "explicit-skill", reports[0].Skill.Manifest.Name)
	assert.True(t, reports[0].Activated)
	assert.Equal(t, "file-skill", reports[1].Skill.Manifest.Name)
}

func TestRuntimeGetActiveSkillsEmpty(t *testing.T) {
	rt, _, _ := newTestRuntime(t, nil, nil)
	active := rt.GetActiveSkills()
	assert.Empty(t, active)
}

func TestRuntimeSnapshotForSubagentFiltersActiveSkills(t *testing.T) {
	rt, _, _ := newTestRuntime(t, []string{"alpha", "beta"}, nil)

	alpha := testManifest("alpha")
	alpha.Types = []SkillType{SkillTypePrompt}
	alpha.Prompt = PromptConfig{SystemPromptFile: "Alpha guidance."}
	rt.loader.RegisterBuiltin(alpha)

	beta := testManifest("beta")
	beta.Types = []SkillType{SkillTypePrompt}
	beta.Prompt = PromptConfig{SystemPromptFile: "Beta guidance."}
	rt.loader.RegisterBuiltin(beta)

	require.NoError(t, rt.Discover(nil))
	require.NoError(t, rt.Activate("alpha"))
	require.NoError(t, rt.Activate("beta"))

	snapshot := rt.SnapshotForSubagent(SubagentSkillPolicy{
		InheritActive: false,
		Include:       []string{"beta"},
	})
	require.NotNil(t, snapshot)

	fragments := snapshot.GetPromptFragments()
	require.Len(t, fragments, 1)
	assert.Equal(t, "beta", fragments[0].SkillName)
	assert.Equal(t, "Beta guidance.", fragments[0].ResolvedPrompt)
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

// --- mock slash command for testing ---

type mockSlashCommand struct {
	name string
}

func (c *mockSlashCommand) Name() string        { return c.name }
func (c *mockSlashCommand) Description() string { return "mock command " + c.name }
func (c *mockSlashCommand) Arguments() []commands.ArgumentDef {
	return nil
}
func (c *mockSlashCommand) Complete(_ context.Context, _ []string) []commands.Candidate {
	return nil
}
func (c *mockSlashCommand) Execute(_ context.Context, _ []string) (commands.Result, error) {
	return commands.Result{}, nil
}

// mockBackendWithCommands embeds mockBackend and overrides Commands() to
// return actual slash commands for testing command registration.
type mockBackendWithCommands struct {
	mockBackend
	cmds []commands.SlashCommand
}

func (m *mockBackendWithCommands) Commands() []commands.SlashCommand {
	return m.cmds
}

func TestRuntimeActivateRegistersCommands(t *testing.T) {
	cmdReg := commands.NewRegistry()

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	registry := tools.NewRegistry()

	testCmd := &mockSlashCommand{name: "test-cmd"}

	backendFactory := func(manifest SkillManifest, dir string) (SkillBackend, error) {
		return &mockBackendWithCommands{
			mockBackend: mockBackend{
				tools: []tools.Tool{
					&runtimeMockTool{name: manifest.Name + "-tool", description: "from " + manifest.Name},
				},
				hooks: map[HookPhase]HookHandler{},
			},
			cmds: []commands.SlashCommand{testCmd},
		}, nil
	}
	sandboxFactory := func(skillName string, declared []Permission) PermissionChecker {
		return &mockPermissionChecker{}
	}

	rt := NewRuntime(NewLoader("", ""), s, registry, []string{"cmd-skill"}, backendFactory, sandboxFactory)
	rt.SetCommandRegistry(cmdReg)

	m := testManifest("cmd-skill")
	rt.loader.RegisterBuiltin(m)
	require.NoError(t, rt.Discover(nil))

	// Before activation, command should not exist.
	_, found := cmdReg.Get("test-cmd")
	assert.False(t, found, "command should not exist before activation")

	// Activate.
	require.NoError(t, rt.Activate("cmd-skill"))

	// After activation, command should be registered.
	cmd, found := cmdReg.Get("test-cmd")
	require.True(t, found, "command should exist after activation")
	assert.Equal(t, "test-cmd", cmd.Name())

	// Deactivate.
	require.NoError(t, rt.Deactivate("cmd-skill"))

	// After deactivation, command should be unregistered.
	_, found = cmdReg.Get("test-cmd")
	assert.False(t, found, "command should be removed after deactivation")
}

func TestRuntimeActivateCommandRegistrationRollback(t *testing.T) {
	cmdReg := commands.NewRegistry()

	// Pre-register a command with the same name to cause a collision.
	preExisting := &mockSlashCommand{name: "collision-cmd"}
	require.NoError(t, cmdReg.Register(preExisting))

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	registry := tools.NewRegistry()

	backendFactory := func(manifest SkillManifest, dir string) (SkillBackend, error) {
		return &mockBackendWithCommands{
			mockBackend: mockBackend{
				tools: []tools.Tool{
					&runtimeMockTool{name: manifest.Name + "-tool", description: "from " + manifest.Name},
				},
				hooks: map[HookPhase]HookHandler{},
			},
			cmds: []commands.SlashCommand{&mockSlashCommand{name: "collision-cmd"}},
		}, nil
	}
	sandboxFactory := func(skillName string, declared []Permission) PermissionChecker {
		return &mockPermissionChecker{}
	}

	rt := NewRuntime(NewLoader("", ""), s, registry, []string{"collision-skill"}, backendFactory, sandboxFactory)
	rt.SetCommandRegistry(cmdReg)

	m := testManifest("collision-skill")
	rt.loader.RegisterBuiltin(m)
	require.NoError(t, rt.Discover(nil))

	// Activate should fail due to command collision.
	err = rt.Activate("collision-skill")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "register command")

	// Skill should be back to Inactive.
	assert.Equal(t, SkillStateInactive, rt.skills["collision-skill"].State)

	// The tool registered before the command collision should be rolled back.
	_, ok := registry.Get("collision-skill-tool")
	assert.False(t, ok, "tool should be rolled back after command registration failure")
}

func TestRuntimeActivateRegistersDeclarativeCommands(t *testing.T) {
	cmdReg := commands.NewRegistry()

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	rt := NewRuntime(NewLoader("", ""), s, tools.NewRegistry(), []string{"instruction-cmd"}, func(manifest SkillManifest, dir string) (SkillBackend, error) {
		return &mockBackend{hooks: map[HookPhase]HookHandler{}}, nil
	}, func(skillName string, declared []Permission) PermissionChecker {
		return &mockPermissionChecker{}
	})
	rt.SetCommandRegistry(cmdReg)

	m := &SkillManifest{
		Name:        "instruction-cmd",
		Version:     "1.0.0",
		Description: "Instruction command skill",
		Types:       []SkillType{SkillTypePrompt},
		Commands: []CommandDef{
			{
				Name:        "review-plan",
				Description: "Draft a review plan",
				Arguments: []CommandArgDef{
					{Name: "scope", Description: "Review scope", Required: true},
				},
			},
		},
	}
	rt.loader.RegisterBuiltin(m)
	require.NoError(t, rt.Discover(nil))

	require.NoError(t, rt.Activate("instruction-cmd"))

	cmd, found := cmdReg.Get("review-plan")
	require.True(t, found)
	assert.Equal(t, "review-plan", cmd.Name())

	_, err = cmd.Execute(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required arguments")

	result, err := cmd.Execute(context.Background(), []string{"ui"})
	require.NoError(t, err)
	assert.Contains(t, result.Output, `Skill command "review-plan" from "instruction-cmd" invoked`)

	require.NoError(t, rt.Deactivate("instruction-cmd"))
	_, found = cmdReg.Get("review-plan")
	assert.False(t, found)
}

// --- mock agent def registrar for testing ---

type mockAgentDefRegistrar struct {
	defs map[string]*AgentDefinition
}

func newMockAgentDefRegistrar() *mockAgentDefRegistrar {
	return &mockAgentDefRegistrar{defs: make(map[string]*AgentDefinition)}
}

func (r *mockAgentDefRegistrar) Register(def *AgentDefinition) error {
	if _, ok := r.defs[def.Name]; ok {
		return fmt.Errorf("agent def %q already registered", def.Name)
	}
	r.defs[def.Name] = def
	return nil
}

func (r *mockAgentDefRegistrar) Unregister(name string) error {
	if _, ok := r.defs[name]; !ok {
		return fmt.Errorf("agent def %q not found", name)
	}
	delete(r.defs, name)
	return nil
}

// mockBackendWithAgents embeds mockBackend and overrides Agents() to
// return actual agent definitions for testing agent def registration.
type mockBackendWithAgents struct {
	mockBackend
	agentDefs []*AgentDefinition
}

func (m *mockBackendWithAgents) Agents() []*AgentDefinition {
	return m.agentDefs
}

func TestRuntimeActivateRegistersAgentDefs(t *testing.T) {
	agentReg := newMockAgentDefRegistrar()

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	registry := tools.NewRegistry()

	testDef := &AgentDefinition{
		Name:         "test-agent",
		Description:  "A test agent",
		SystemPrompt: "You are a test agent.",
		Tools:        []string{"file", "search"},
		MaxTurns:     5,
	}

	backendFactory := func(manifest SkillManifest, dir string) (SkillBackend, error) {
		return &mockBackendWithAgents{
			mockBackend: mockBackend{
				tools: []tools.Tool{
					&runtimeMockTool{name: manifest.Name + "-tool", description: "from " + manifest.Name},
				},
				hooks: map[HookPhase]HookHandler{},
			},
			agentDefs: []*AgentDefinition{testDef},
		}, nil
	}
	sandboxFactory := func(skillName string, declared []Permission) PermissionChecker {
		return &mockPermissionChecker{}
	}

	rt := NewRuntime(NewLoader("", ""), s, registry, []string{"agent-skill"}, backendFactory, sandboxFactory)
	rt.SetAgentDefRegistrar(agentReg)

	m := testManifest("agent-skill")
	rt.loader.RegisterBuiltin(m)
	require.NoError(t, rt.Discover(nil))

	// Before activation, agent def should not exist.
	_, exists := agentReg.defs["test-agent"]
	assert.False(t, exists, "agent def should not exist before activation")

	// Activate.
	require.NoError(t, rt.Activate("agent-skill"))

	// After activation, agent def should be registered.
	def, exists := agentReg.defs["test-agent"]
	require.True(t, exists, "agent def should exist after activation")
	assert.Equal(t, "test-agent", def.Name)
	assert.Equal(t, "A test agent", def.Description)

	// Deactivate.
	require.NoError(t, rt.Deactivate("agent-skill"))

	// After deactivation, agent def should be unregistered.
	_, exists = agentReg.defs["test-agent"]
	assert.False(t, exists, "agent def should be removed after deactivation")
}

func TestRuntimeActivateAgentDefRegistrationRollback(t *testing.T) {
	agentReg := newMockAgentDefRegistrar()

	// Pre-register an agent def with the same name to cause a collision.
	agentReg.defs["collision-agent"] = &AgentDefinition{Name: "collision-agent"}

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	registry := tools.NewRegistry()

	backendFactory := func(manifest SkillManifest, dir string) (SkillBackend, error) {
		return &mockBackendWithAgents{
			mockBackend: mockBackend{
				tools: []tools.Tool{
					&runtimeMockTool{name: manifest.Name + "-tool", description: "from " + manifest.Name},
				},
				hooks: map[HookPhase]HookHandler{},
			},
			agentDefs: []*AgentDefinition{{Name: "collision-agent"}},
		}, nil
	}
	sandboxFactory := func(skillName string, declared []Permission) PermissionChecker {
		return &mockPermissionChecker{}
	}

	rt := NewRuntime(NewLoader("", ""), s, registry, []string{"collision-agent-skill"}, backendFactory, sandboxFactory)
	rt.SetAgentDefRegistrar(agentReg)

	m := testManifest("collision-agent-skill")
	rt.loader.RegisterBuiltin(m)
	require.NoError(t, rt.Discover(nil))

	// Activate should fail due to agent def collision.
	err = rt.Activate("collision-agent-skill")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "register agent def")

	// Skill should be back to Inactive.
	assert.Equal(t, SkillStateInactive, rt.skills["collision-agent-skill"].State)

	// The tool registered before the agent def collision should be rolled back.
	_, ok := registry.Get("collision-agent-skill-tool")
	assert.False(t, ok, "tool should be rolled back after agent def registration failure")
}

func TestGetAllSkillSummaries(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	rt := NewRuntime(NewLoader("", ""), s, tools.NewRegistry(), nil,
		func(manifest SkillManifest, dir string) (SkillBackend, error) {
			return &mockBackend{hooks: map[HookPhase]HookHandler{}}, nil
		},
		func(skillName string, declared []Permission) PermissionChecker {
			return &mockPermissionChecker{}
		},
	)

	// Register two skills with different sources.
	rt.loader.RegisterBuiltin(&SkillManifest{
		Name:        "alpha",
		Version:     "1.0.0",
		Description: "Alpha skill",
		Types:       []SkillType{SkillTypePrompt},
	})
	rt.loader.RegisterBuiltin(&SkillManifest{
		Name:        "beta",
		Version:     "1.0.0",
		Description: "Beta skill",
		Types:       []SkillType{SkillTypeTool},
	})
	require.NoError(t, rt.Discover(nil))

	// Activate alpha.
	require.NoError(t, rt.Activate("alpha"))

	summaries := rt.GetAllSkillSummaries()

	// All discovered skills should be included.
	require.Len(t, summaries, 2)

	// Results should be sorted by name.
	assert.Equal(t, "alpha", summaries[0].Name)
	assert.Equal(t, "beta", summaries[1].Name)

	// Active skill shows Active state.
	assert.Equal(t, SkillStateActive, summaries[0].State)
	assert.Equal(t, "Alpha skill", summaries[0].Description)
	assert.Equal(t, SourceBuiltin, summaries[0].Source)
	assert.Equal(t, []SkillType{SkillTypePrompt}, summaries[0].Types)

	// Inactive skill shows Inactive state.
	assert.Equal(t, SkillStateInactive, summaries[1].State)
	assert.Equal(t, "Beta skill", summaries[1].Description)
}

func TestGetAllSkillSummariesEmpty(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	rt := NewRuntime(NewLoader("", ""), s, tools.NewRegistry(), nil, nil, nil)

	summaries := rt.GetAllSkillSummaries()
	assert.Empty(t, summaries)
}

func TestRuntimeActivateRegistersDeclarativeAgentDefs(t *testing.T) {
	agentReg := newMockAgentDefRegistrar()

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	rt := NewRuntime(NewLoader("", ""), s, tools.NewRegistry(), []string{"instruction-agent"}, func(manifest SkillManifest, dir string) (SkillBackend, error) {
		return &mockBackend{hooks: map[HookPhase]HookHandler{}}, nil
	}, func(skillName string, declared []Permission) PermissionChecker {
		return &mockPermissionChecker{}
	})
	rt.SetAgentDefRegistrar(agentReg)

	m := &SkillManifest{
		Name:        "instruction-agent",
		Version:     "1.0.0",
		Description: "Instruction agent skill",
		Types:       []SkillType{SkillTypePrompt},
		Agents: []AgentDefManifest{
			{
				Name:         "review-agent",
				Description:  "Focused reviewer",
				SystemPrompt: "Review thoroughly.",
				Tools:        []string{"read_file"},
				MaxTurns:     4,
				MaxDepth:     2,
				Model:        "test-model",
			},
		},
	}
	rt.loader.RegisterBuiltin(m)
	require.NoError(t, rt.Discover(nil))

	require.NoError(t, rt.Activate("instruction-agent"))

	def, found := agentReg.defs["review-agent"]
	require.True(t, found)
	assert.Equal(t, "Focused reviewer", def.Description)
	assert.Equal(t, "Review thoroughly.", def.SystemPrompt)
	assert.Equal(t, []string{"read_file"}, def.Tools)
	assert.Equal(t, 4, def.MaxTurns)
	assert.Equal(t, 2, def.MaxDepth)
	assert.Equal(t, "test-model", def.Model)

	require.NoError(t, rt.Deactivate("instruction-agent"))
	_, found = agentReg.defs["review-agent"]
	assert.False(t, found)
}
