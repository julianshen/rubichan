package skills

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- integration test helpers ---

// integrationMockTool is a minimal tool for integration tests.
type integrationMockTool struct {
	name        string
	description string
}

func (t *integrationMockTool) Name() string        { return t.name }
func (t *integrationMockTool) Description() string { return t.description }
func (t *integrationMockTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (t *integrationMockTool) Execute(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{Content: "ok"}, nil
}

// newIntegrationRuntime creates a Runtime for integration tests. The caller
// provides a custom BackendFactory so each test can wire unique backends.
func newIntegrationRuntime(t *testing.T, autoApprove []string, bf BackendFactory) *Runtime {
	t.Helper()

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })

	registry := tools.NewRegistry()
	sandboxFactory := func(skillName string, declared []Permission) PermissionChecker {
		return &mockPermissionChecker{}
	}

	rt := NewRuntime(NewLoader("", ""), s, registry, autoApprove, bf, sandboxFactory)
	return rt
}

// TestToolSkillRegistersTools verifies that a skill with type "tool" has its
// tools appear in the registry after activation.
func TestToolSkillRegistersTools(t *testing.T) {
	toolA := &integrationMockTool{name: "alpha-tool", description: "alpha"}
	toolB := &integrationMockTool{name: "beta-tool", description: "beta"}

	bf := func(manifest SkillManifest) (SkillBackend, error) {
		return &mockBackend{
			tools: []tools.Tool{toolA, toolB},
			hooks: map[HookPhase]HookHandler{},
		}, nil
	}

	rt := newIntegrationRuntime(t, []string{"tool-skill"}, bf)

	m := &SkillManifest{
		Name:        "tool-skill",
		Version:     "1.0.0",
		Description: "A tool skill",
		Types:       []SkillType{SkillTypeTool},
		Implementation: ImplementationConfig{
			Backend:    BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	rt.loader.RegisterBuiltin(m)
	require.NoError(t, rt.Discover(nil))

	// Before activation, tools should not be registered.
	_, ok := rt.registry.Get("alpha-tool")
	assert.False(t, ok, "tool should not exist before activation")

	// Activate the skill.
	require.NoError(t, rt.Activate("tool-skill"))

	// Both tools should now be in the registry.
	got, ok := rt.registry.Get("alpha-tool")
	require.True(t, ok, "alpha-tool should be registered after activation")
	assert.Equal(t, "alpha-tool", got.Name())

	got, ok = rt.registry.Get("beta-tool")
	require.True(t, ok, "beta-tool should be registered after activation")
	assert.Equal(t, "beta-tool", got.Name())
}

// TestPromptSkillInjectsFragment verifies that a skill with type "prompt"
// registers a HookOnBeforePromptBuild hook that injects the prompt fragment
// into the event data.
func TestPromptSkillInjectsFragment(t *testing.T) {
	bf := func(manifest SkillManifest) (SkillBackend, error) {
		return &mockBackend{
			tools: []tools.Tool{},
			hooks: map[HookPhase]HookHandler{},
		}, nil
	}

	rt := newIntegrationRuntime(t, []string{"prompt-skill"}, bf)

	m := &SkillManifest{
		Name:        "prompt-skill",
		Version:     "1.0.0",
		Description: "A prompt skill",
		Types:       []SkillType{SkillTypePrompt},
		Prompt: PromptConfig{
			SystemPromptFile: "system.md",
			ContextFiles:     []string{"context.md"},
			MaxContextTokens: 2000,
		},
	}
	rt.loader.RegisterBuiltin(m)
	require.NoError(t, rt.Discover(nil))

	// Activate the prompt skill.
	require.NoError(t, rt.Activate("prompt-skill"))

	// The PromptCollector should contain the prompt fragment.
	fragments := rt.GetPromptFragments()
	require.Len(t, fragments, 1)
	assert.Equal(t, "prompt-skill", fragments[0].SkillName)
	assert.Equal(t, "system.md", fragments[0].SystemPromptFile)

	// Dispatch a HookOnBeforePromptBuild event and verify the fragment is injected.
	result, err := rt.lifecycle.Dispatch(HookEvent{
		Phase: HookOnBeforePromptBuild,
		Data:  map[string]any{},
		Ctx:   context.Background(),
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	// For builtin skills (Dir=""), SystemPromptFile is used as inline content.
	assert.Equal(t, "system.md", result.Modified["prompt_fragment"].(string))
}

// TestWorkflowSkillInvokable verifies that a skill with type "workflow"
// stores a handler in WorkflowRunner that can be invoked by name.
func TestWorkflowSkillInvokable(t *testing.T) {
	workflowCalled := false

	bf := func(manifest SkillManifest) (SkillBackend, error) {
		return &mockBackend{
			tools: []tools.Tool{},
			hooks: map[HookPhase]HookHandler{
				HookOnActivate: func(event HookEvent) (HookResult, error) {
					return HookResult{}, nil
				},
			},
		}, nil
	}

	rt := newIntegrationRuntime(t, []string{"workflow-skill"}, bf)

	m := &SkillManifest{
		Name:        "workflow-skill",
		Version:     "1.0.0",
		Description: "A workflow skill",
		Types:       []SkillType{SkillTypeWorkflow},
		Implementation: ImplementationConfig{
			Backend:    BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	rt.loader.RegisterBuiltin(m)
	require.NoError(t, rt.Discover(nil))

	// Register a workflow handler on the runtime before activation.
	rt.workflowRunner.Register("workflow-skill", func(ctx context.Context, args map[string]any) (map[string]any, error) {
		workflowCalled = true
		return map[string]any{"status": "done"}, nil
	})

	// Activate the skill.
	require.NoError(t, rt.Activate("workflow-skill"))

	// Invoke the workflow by name.
	result, err := rt.InvokeWorkflow(context.Background(), "workflow-skill", nil)
	require.NoError(t, err)
	assert.True(t, workflowCalled, "workflow handler should have been called")
	assert.Equal(t, "done", result["status"])

	// Invoking a non-existent workflow should return an error.
	_, err = rt.InvokeWorkflow(context.Background(), "nonexistent", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestSecurityRuleSkillRegisters verifies that a skill with type "security-rule"
// registers its scanner via the SecurityRuleAdapter.
func TestSecurityRuleSkillRegisters(t *testing.T) {
	scannerCalled := false

	bf := func(manifest SkillManifest) (SkillBackend, error) {
		return &mockBackend{
			tools: []tools.Tool{},
			hooks: map[HookPhase]HookHandler{},
		}, nil
	}

	rt := newIntegrationRuntime(t, []string{"security-skill"}, bf)

	m := &SkillManifest{
		Name:        "security-skill",
		Version:     "1.0.0",
		Description: "A security-rule skill",
		Types:       []SkillType{SkillTypeSecurityRule},
		Implementation: ImplementationConfig{
			Backend:    BackendStarlark,
			Entrypoint: "main.star",
		},
		SecurityRules: SecurityRuleConfig{
			Scanners: []ScannerDef{
				{Name: "secret-scanner", Entrypoint: "scan.star"},
			},
		},
	}
	rt.loader.RegisterBuiltin(m)
	require.NoError(t, rt.Discover(nil))

	// Pre-register scanner function for this skill.
	rt.securityAdapter.RegisterScanner("security-skill", "secret-scanner", func(ctx context.Context, content string) ([]SecurityFinding, error) {
		scannerCalled = true
		return []SecurityFinding{{Rule: "secret-scanner", Message: "found secret", Severity: "high"}}, nil
	})

	// Activate the security-rule skill.
	require.NoError(t, rt.Activate("security-skill"))

	// Retrieve scanners from the adapter.
	scanners := rt.GetScanners()
	require.Len(t, scanners, 1)
	assert.Equal(t, "secret-scanner", scanners[0].Name)

	// Run the scanner and verify it works.
	findings, err := scanners[0].Scan(context.Background(), "some source code")
	require.NoError(t, err)
	assert.True(t, scannerCalled)
	require.Len(t, findings, 1)
	assert.Equal(t, "found secret", findings[0].Message)
}

// TestTransformSkillModifiesOutput verifies that a skill with type "transform"
// registers an OnAfterResponse hook that modifies the response.
func TestTransformSkillModifiesOutput(t *testing.T) {
	bf := func(manifest SkillManifest) (SkillBackend, error) {
		return &mockBackend{
			tools: []tools.Tool{},
			hooks: map[HookPhase]HookHandler{
				HookOnAfterResponse: func(event HookEvent) (HookResult, error) {
					original := event.Data["response"].(string)
					return HookResult{
						Modified: map[string]any{
							"response": original + " [transformed]",
						},
					}, nil
				},
			},
		}, nil
	}

	rt := newIntegrationRuntime(t, []string{"transform-skill"}, bf)

	m := &SkillManifest{
		Name:        "transform-skill",
		Version:     "1.0.0",
		Description: "A transform skill",
		Types:       []SkillType{SkillTypeTransform},
		Implementation: ImplementationConfig{
			Backend:    BackendStarlark,
			Entrypoint: "main.star",
		},
	}
	rt.loader.RegisterBuiltin(m)
	require.NoError(t, rt.Discover(nil))

	// Activate the transform skill.
	require.NoError(t, rt.Activate("transform-skill"))

	// Dispatch an OnAfterResponse event.
	result, err := rt.lifecycle.Dispatch(HookEvent{
		Phase: HookOnAfterResponse,
		Data:  map[string]any{"response": "hello world"},
		Ctx:   context.Background(),
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "hello world [transformed]", result.Modified["response"])
}

// TestMultiTypeSkill verifies that a skill with types: [tool, prompt] registers
// both tools and the prompt fragment.
func TestMultiTypeSkill(t *testing.T) {
	myTool := &integrationMockTool{name: "multi-tool", description: "multi"}

	bf := func(manifest SkillManifest) (SkillBackend, error) {
		return &mockBackend{
			tools: []tools.Tool{myTool},
			hooks: map[HookPhase]HookHandler{},
		}, nil
	}

	rt := newIntegrationRuntime(t, []string{"multi-skill"}, bf)

	m := &SkillManifest{
		Name:        "multi-skill",
		Version:     "1.0.0",
		Description: "A multi-type skill",
		Types:       []SkillType{SkillTypeTool, SkillTypePrompt},
		Implementation: ImplementationConfig{
			Backend:    BackendStarlark,
			Entrypoint: "main.star",
		},
		Prompt: PromptConfig{
			SystemPromptFile: "multi-system.md",
			ContextFiles:     []string{"ctx.md"},
			MaxContextTokens: 1000,
		},
	}
	rt.loader.RegisterBuiltin(m)
	require.NoError(t, rt.Discover(nil))

	// Activate the multi-type skill.
	require.NoError(t, rt.Activate("multi-skill"))

	// Tool should be registered (tool type).
	got, ok := rt.registry.Get("multi-tool")
	require.True(t, ok, "tool should be registered for multi-type skill")
	assert.Equal(t, "multi-tool", got.Name())

	// Prompt fragment should be registered (prompt type).
	fragments := rt.GetPromptFragments()
	require.Len(t, fragments, 1)
	assert.Equal(t, "multi-skill", fragments[0].SkillName)
	assert.Equal(t, "multi-system.md", fragments[0].SystemPromptFile)

	// Dispatch a HookOnBeforePromptBuild and verify injection.
	result, err := rt.lifecycle.Dispatch(HookEvent{
		Phase: HookOnBeforePromptBuild,
		Data:  map[string]any{},
		Ctx:   context.Background(),
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	// For builtin skills (Dir=""), SystemPromptFile is used as inline content.
	assert.Equal(t, "multi-system.md", result.Modified["prompt_fragment"].(string))
}

// TestDeactivateCleanupIntegrations verifies that deactivating a skill with
// prompt, workflow, and security-rule types properly cleans up all integration
// state (prompt fragments, workflow handlers, and scanners).
func TestDeactivateCleanupIntegrations(t *testing.T) {
	bf := func(manifest SkillManifest) (SkillBackend, error) {
		return &mockBackend{
			tools: []tools.Tool{},
			hooks: map[HookPhase]HookHandler{},
		}, nil
	}

	rt := newIntegrationRuntime(t, []string{"cleanup-skill"}, bf)

	m := &SkillManifest{
		Name:        "cleanup-skill",
		Version:     "1.0.0",
		Description: "A skill with multiple types for cleanup testing",
		Types:       []SkillType{SkillTypePrompt, SkillTypeWorkflow, SkillTypeSecurityRule},
		Prompt: PromptConfig{
			SystemPromptFile: "cleanup-system.md",
		},
		Implementation: ImplementationConfig{
			Backend:    BackendStarlark,
			Entrypoint: "main.star",
		},
		SecurityRules: SecurityRuleConfig{
			Scanners: []ScannerDef{{Name: "cleanup-scanner", Entrypoint: "scan.star"}},
		},
	}
	rt.loader.RegisterBuiltin(m)
	require.NoError(t, rt.Discover(nil))

	// Pre-register workflow and scanner.
	rt.workflowRunner.Register("cleanup-skill", func(ctx context.Context, args map[string]any) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	})
	rt.securityAdapter.RegisterScanner("cleanup-skill", "cleanup-scanner", func(ctx context.Context, content string) ([]SecurityFinding, error) {
		return nil, nil
	})

	// Activate.
	require.NoError(t, rt.Activate("cleanup-skill"))

	// Verify integrations are present.
	assert.Len(t, rt.GetPromptFragments(), 1)
	assert.Len(t, rt.GetScanners(), 1)
	_, err := rt.InvokeWorkflow(context.Background(), "cleanup-skill", nil)
	require.NoError(t, err)

	// Deactivate should clean up all integrations.
	require.NoError(t, rt.Deactivate("cleanup-skill"))

	// All integrations should be cleaned up.
	assert.Empty(t, rt.GetPromptFragments(), "prompt fragments should be cleaned up after deactivation")
	assert.Empty(t, rt.GetScanners(), "scanners should be cleaned up after deactivation")
	_, err = rt.InvokeWorkflow(context.Background(), "cleanup-skill", nil)
	require.Error(t, err, "workflow should be cleaned up after deactivation")
	assert.Contains(t, err.Error(), "not found")
}
