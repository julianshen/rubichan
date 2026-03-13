package agent

import (
	"context"
	"os"
	"testing"

	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mock types for skill runtime tests ---

// skillMockBackend is a mock SkillBackend for testing.
type skillMockBackend struct {
	backendTools []tools.Tool
	hooks        map[skills.HookPhase]skills.HookHandler
}

func (m *skillMockBackend) Load(_ skills.SkillManifest, _ skills.PermissionChecker) error {
	return nil
}
func (m *skillMockBackend) Tools() []tools.Tool { return m.backendTools }
func (m *skillMockBackend) Hooks() map[skills.HookPhase]skills.HookHandler {
	if m.hooks == nil {
		return map[skills.HookPhase]skills.HookHandler{}
	}
	return m.hooks
}
func (m *skillMockBackend) Commands() []commands.SlashCommand { return nil }
func (m *skillMockBackend) Agents() []*skills.AgentDefinition { return nil }
func (m *skillMockBackend) Unload() error                     { return nil }

// skillMockChecker is a mock PermissionChecker that always approves.
type skillMockChecker struct{}

func (m *skillMockChecker) CheckPermission(_ skills.Permission) error { return nil }
func (m *skillMockChecker) CheckRateLimit(_ string) error             { return nil }
func (m *skillMockChecker) ResetTurnLimits()                          {}

// capturingMockProvider captures the CompletionRequest for inspection.
type capturingMockProvider struct {
	events     []provider.StreamEvent
	captureReq *provider.CompletionRequest
}

func (c *capturingMockProvider) Stream(_ context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	if c.captureReq != nil {
		*c.captureReq = req
	}
	ch := make(chan provider.StreamEvent, len(c.events))
	for _, e := range c.events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

type requestCapturingProvider struct {
	events    []provider.StreamEvent
	onRequest func(provider.CompletionRequest)
}

func (p *requestCapturingProvider) Stream(_ context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	if p.onRequest != nil {
		p.onRequest(req)
	}
	ch := make(chan provider.StreamEvent, len(p.events))
	for _, e := range p.events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

// makeTestRuntime creates a skills.Runtime with a built-in skill that has
// the given hooks. It discovers and activates the skill.
func makeTestRuntime(t *testing.T, skillName string, manifest *skills.SkillManifest, backendTools []tools.Tool, hooks map[skills.HookPhase]skills.HookHandler) *skills.Runtime {
	t.Helper()

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })

	registry := tools.NewRegistry()

	backendFactory := func(_ skills.SkillManifest, _ string) (skills.SkillBackend, error) {
		return &skillMockBackend{
			backendTools: backendTools,
			hooks:        hooks,
		}, nil
	}
	sandboxFactory := func(_ string, _ []skills.Permission) skills.PermissionChecker {
		return &skillMockChecker{}
	}

	loader := skills.NewLoader("", "")
	loader.RegisterBuiltin(manifest)

	rt := skills.NewRuntime(loader, s, registry, []string{skillName}, backendFactory, sandboxFactory)
	require.NoError(t, rt.Discover(nil))
	require.NoError(t, rt.Activate(skillName))

	return rt
}

func makeTriggeredRuntime(t *testing.T, manifest *skills.SkillManifest, registry *tools.Registry, backendTools []tools.Tool) *skills.Runtime {
	t.Helper()

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })

	if registry == nil {
		registry = tools.NewRegistry()
	}
	backendFactory := func(_ skills.SkillManifest, _ string) (skills.SkillBackend, error) {
		return &skillMockBackend{backendTools: backendTools, hooks: map[skills.HookPhase]skills.HookHandler{}}, nil
	}
	sandboxFactory := func(_ string, _ []skills.Permission) skills.PermissionChecker {
		return &skillMockChecker{}
	}

	loader := skills.NewLoader("", "")
	loader.RegisterBuiltin(manifest)

	rt := skills.NewRuntime(loader, s, registry, nil, backendFactory, sandboxFactory)
	require.NoError(t, rt.Discover(nil))
	return rt
}

// toolManifest returns a minimal valid SkillManifest for a tool skill.
func toolManifest(name string) *skills.SkillManifest {
	return &skills.SkillManifest{
		Name:        name,
		Version:     "1.0.0",
		Description: "Test skill " + name,
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
		Permissions: []skills.Permission{skills.PermFileRead},
	}
}

// --- Tests ---

// TestAgentWithSkillRuntime verifies that an agent accepts a skill runtime
// and that skill tools appear in completions.
func TestAgentWithSkillRuntime(t *testing.T) {
	rt := makeTestRuntime(t, "agent-skill", toolManifest("agent-skill"), nil, nil)

	// The mock provider captures the completion request so we can inspect it.
	var capturedReq provider.CompletionRequest
	cp := &capturingMockProvider{
		events: []provider.StreamEvent{
			{Type: "text_delta", Text: "Using skill tool"},
			{Type: "stop"},
		},
		captureReq: &capturedReq,
	}

	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	a := New(cp, reg, autoApprove, cfg, WithSkillRuntime(rt))

	require.NotNil(t, a)
	require.NotNil(t, a.skillRuntime)

	// Run a turn.
	ch, err := a.Turn(context.Background(), "hello")
	require.NoError(t, err)

	for range ch {
		// drain
	}

	// Verify the skill runtime was accepted.
	assert.Equal(t, rt, a.skillRuntime)
}

// TestAgentBeforeToolCallHook verifies that a HookOnBeforeToolCall hook can
// intercept and cancel a tool call.
func TestAgentBeforeToolCallHook(t *testing.T) {
	hookCalled := false
	hooks := map[skills.HookPhase]skills.HookHandler{
		skills.HookOnBeforeToolCall: func(event skills.HookEvent) (skills.HookResult, error) {
			hookCalled = true
			// Verify the hook receives the tool name.
			assert.Equal(t, "file", event.Data["tool_name"])
			return skills.HookResult{Cancel: true}, nil
		},
	}

	rt := makeTestRuntime(t, "cancel-hook", toolManifest("cancel-hook"), nil, hooks)

	// Create a provider that returns a tool call.
	dmp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
					ID:   "tool_cancel_1",
					Name: "file",
				}},
				{Type: "text_delta", Text: `{"operation":"read","path":"test.txt"}`},
				{Type: "stop"},
			},
			{
				{Type: "text_delta", Text: "Tool was cancelled."},
				{Type: "stop"},
			},
		},
	}

	agentReg := tools.NewRegistry()
	fileTool := tools.NewFileTool(t.TempDir())
	require.NoError(t, agentReg.Register(fileTool))

	cfg := config.DefaultConfig()
	a := New(dmp, agentReg, autoApprove, cfg, WithSkillRuntime(rt))

	ch, err := a.Turn(context.Background(), "read test.txt")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// The hook should have been called and cancelled the tool call.
	assert.True(t, hookCalled, "before-tool-call hook should have been invoked")

	// Verify that the tool result contains the cancellation message.
	var hasCancelResult bool
	for _, ev := range events {
		if ev.Type == "tool_result" && ev.ToolResult != nil {
			if ev.ToolResult.IsError {
				assert.Contains(t, ev.ToolResult.Content, "cancelled by skill")
				hasCancelResult = true
			}
		}
	}
	assert.True(t, hasCancelResult, "should have a cancelled tool result")
}

// TestAgentAfterToolResultHook verifies that a HookOnAfterToolResult hook can
// modify the tool result content.
func TestAgentAfterToolResultHook(t *testing.T) {
	hookCalled := false
	hooks := map[skills.HookPhase]skills.HookHandler{
		skills.HookOnAfterToolResult: func(event skills.HookEvent) (skills.HookResult, error) {
			hookCalled = true
			return skills.HookResult{
				Modified: map[string]any{
					"content": "modified-by-hook",
				},
			}, nil
		},
	}

	rt := makeTestRuntime(t, "result-hook", toolManifest("result-hook"), nil, hooks)

	// Create a temp file for the tool to read.
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(tmpDir+"/hello.txt", []byte("original content"), 0644))

	// Provider: first call returns tool use, second call returns text.
	dmp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
					ID:   "tool_mod_1",
					Name: "file",
				}},
				{Type: "text_delta", Text: `{"operation":"read","path":"hello.txt"}`},
				{Type: "stop"},
			},
			{
				{Type: "text_delta", Text: "Done."},
				{Type: "stop"},
			},
		},
	}

	agentReg := tools.NewRegistry()
	fileTool := tools.NewFileTool(tmpDir)
	require.NoError(t, agentReg.Register(fileTool))

	cfg := config.DefaultConfig()
	a := New(dmp, agentReg, autoApprove, cfg, WithSkillRuntime(rt))

	ch, err := a.Turn(context.Background(), "read hello.txt")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	assert.True(t, hookCalled, "after-tool-result hook should have been invoked")

	// Verify that the tool result was modified by the hook.
	var toolResultContent string
	for _, ev := range events {
		if ev.Type == "tool_result" && ev.ToolResult != nil && !ev.ToolResult.IsError {
			toolResultContent = ev.ToolResult.Content
		}
	}
	assert.Equal(t, "modified-by-hook", toolResultContent, "tool result should be modified by hook")
}

// TestAgentPromptInjection verifies that prompt fragments from the skill
// runtime are included in the system prompt.
func TestAgentPromptInjection(t *testing.T) {
	// Build a runtime with a prompt skill that contributes a fragment.
	// For builtin skills (Dir=""), SystemPromptFile is used as inline content.
	promptManifest := &skills.SkillManifest{
		Name:        "prompt-skill",
		Version:     "1.0.0",
		Description: "Prompt test skill",
		Types:       []skills.SkillType{skills.SkillTypePrompt},
		Prompt: skills.PromptConfig{
			SystemPromptFile: "You are a security expert. Always check for vulnerabilities.",
		},
	}

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })

	registry := tools.NewRegistry()

	backendFactory := func(_ skills.SkillManifest, _ string) (skills.SkillBackend, error) {
		return &skillMockBackend{
			hooks: map[skills.HookPhase]skills.HookHandler{},
		}, nil
	}
	sandboxFactory := func(_ string, _ []skills.Permission) skills.PermissionChecker {
		return &skillMockChecker{}
	}

	loader := skills.NewLoader("", "")
	loader.RegisterBuiltin(promptManifest)

	rt := skills.NewRuntime(loader, s, registry, []string{"prompt-skill"}, backendFactory, sandboxFactory)
	require.NoError(t, rt.Discover(nil))
	require.NoError(t, rt.Activate("prompt-skill"))

	// Verify prompt fragments are available.
	fragments := rt.GetPromptFragments()
	require.Len(t, fragments, 1)
	assert.Equal(t, "You are a security expert. Always check for vulnerabilities.", fragments[0].ResolvedPrompt)

	// Create a capturing provider to inspect the system prompt.
	var capturedReq provider.CompletionRequest
	cp := &capturingMockProvider{
		events: []provider.StreamEvent{
			{Type: "text_delta", Text: "OK"},
			{Type: "stop"},
		},
		captureReq: &capturedReq,
	}

	agentReg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	a := New(cp, agentReg, autoApprove, cfg, WithSkillRuntime(rt))

	ch, err := a.Turn(context.Background(), "check security")
	require.NoError(t, err)

	for range ch {
		// drain
	}

	// The system prompt should include the prompt fragment from the skill.
	assert.Contains(t, capturedReq.System, "security expert",
		"system prompt should include prompt fragment from skill runtime")
}

func TestAgentTurnEvaluatesKeywordTriggersForPromptSkills(t *testing.T) {
	promptManifest := &skills.SkillManifest{
		Name:        "keyword-prompt-skill",
		Version:     "1.0.0",
		Description: "Prompt activated by keyword",
		Types:       []skills.SkillType{skills.SkillTypePrompt},
		Triggers:    skills.TriggerConfig{Keywords: []string{"security"}},
		Prompt: skills.PromptConfig{
			SystemPromptFile: "Security-specific guidance.",
		},
	}

	rt := makeTriggeredRuntime(t, promptManifest, nil, nil)

	var reqs []provider.CompletionRequest
	cp := &requestCapturingProvider{
		events: []provider.StreamEvent{{Type: "stop"}},
		onRequest: func(req provider.CompletionRequest) {
			reqs = append(reqs, req)
		},
	}

	agentReg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	a := New(cp, agentReg, autoApprove, cfg, WithSkillRuntime(rt), WithMode("interactive"))

	ch, err := a.Turn(context.Background(), "hello there")
	require.NoError(t, err)
	for range ch {
	}
	require.Len(t, reqs, 1)
	assert.NotContains(t, reqs[0].System, "Security-specific guidance.")

	ch, err = a.Turn(context.Background(), "please run a security review")
	require.NoError(t, err)
	for range ch {
	}
	require.Len(t, reqs, 2)
	assert.Contains(t, reqs[1].System, "Security-specific guidance.")
}

func TestAgentTurnEvaluatesKeywordTriggersForToolContributions(t *testing.T) {
	triggeredTool := &mockTool{name: "skill-triggered", description: "skill tool"}
	manifest := &skills.SkillManifest{
		Name:        "keyword-tool-skill",
		Version:     "1.0.0",
		Description: "Tool activated by keyword",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Triggers:    skills.TriggerConfig{Keywords: []string{"deploy"}},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendStarlark,
			Entrypoint: "main.star",
		},
		Permissions: []skills.Permission{skills.PermFileRead},
	}

	agentReg := tools.NewRegistry()
	rt := makeTriggeredRuntime(t, manifest, agentReg, []tools.Tool{triggeredTool})

	var reqs []provider.CompletionRequest
	cp := &requestCapturingProvider{
		events: []provider.StreamEvent{{Type: "stop"}},
		onRequest: func(req provider.CompletionRequest) {
			reqs = append(reqs, req)
		},
	}

	cfg := config.DefaultConfig()
	a := New(cp, agentReg, autoApprove, cfg, WithSkillRuntime(rt), WithMode("interactive"))

	ch, err := a.Turn(context.Background(), "hello there")
	require.NoError(t, err)
	for range ch {
	}
	require.Len(t, reqs, 1)
	assert.NotContains(t, toolNames(reqs[0].Tools), "skill-triggered")

	ch, err = a.Turn(context.Background(), "deploy this service")
	require.NoError(t, err)
	for range ch {
	}
	require.Len(t, reqs, 2)
	assert.Contains(t, toolNames(reqs[1].Tools), "skill-triggered")
}

func toolNames(defs []provider.ToolDef) []string {
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		names = append(names, def.Name)
	}
	return names
}
