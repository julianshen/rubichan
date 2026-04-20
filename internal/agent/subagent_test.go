package agent

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Task 6 tests

func TestSubagentConfigDefaults(t *testing.T) {
	cfg := SubagentConfig{Name: "test"}
	assert.Equal(t, 0, cfg.MaxTurns)
	assert.Equal(t, 0, cfg.Depth)
	assert.Equal(t, 0, cfg.MaxDepth)
	assert.Nil(t, cfg.Tools)
}

func TestSubagentResultFields(t *testing.T) {
	result := SubagentResult{
		Name:         "explorer",
		Output:       "Found 3 files",
		ToolsUsed:    []string{"search", "file"},
		TurnCount:    2,
		InputTokens:  1500,
		OutputTokens: 300,
	}
	assert.Equal(t, "explorer", result.Name)
	assert.Equal(t, 2, result.TurnCount)
	assert.Nil(t, result.Error)
}

// Task 7 tests

func TestDefaultSubagentSpawnerMaxDepth(t *testing.T) {
	spawner := &DefaultSubagentSpawner{}
	cfg := SubagentConfig{Depth: 3, MaxDepth: 3}
	_, err := spawner.Spawn(context.Background(), cfg, "hello")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "depth")
}

func TestDefaultSubagentSpawnerNoProvider(t *testing.T) {
	spawner := &DefaultSubagentSpawner{}
	cfg := SubagentConfig{Name: "test"}
	_, err := spawner.Spawn(context.Background(), cfg, "hello")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "provider")
}

func TestMockSpawnerSatisfiesInterface(t *testing.T) {
	var _ SubagentSpawner = (*DefaultSubagentSpawner)(nil)
}

func TestDefaultSubagentSpawnerWorktreeIsolation_NoProvider(t *testing.T) {
	recorder := &recordingProvider{}
	spawner := &DefaultSubagentSpawner{
		Provider:    recorder,
		ParentTools: tools.NewRegistry(),
		Config:      &config.Config{Provider: config.ProviderConfig{Model: "test"}},
		// WorktreeProvider intentionally nil
	}
	_, err := spawner.Spawn(context.Background(), SubagentConfig{
		Name:      "isolated",
		Isolation: "worktree",
	}, "hello")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "WorktreeProvider")
}

func TestDefaultSubagentSpawnerWorktreeIsolation_Success(t *testing.T) {
	recorder := &recordingProvider{}
	mockWT := &mockWorktreeProvider{dir: t.TempDir()}
	spawner := &DefaultSubagentSpawner{
		Provider:         recorder,
		ParentTools:      tools.NewRegistry(),
		Config:           &config.Config{Provider: config.ProviderConfig{Model: "test"}},
		WorktreeProvider: mockWT,
	}
	result, err := spawner.Spawn(context.Background(), SubagentConfig{
		Name:      "worker",
		Isolation: "worktree",
	}, "hello")
	require.NoError(t, err)
	assert.Equal(t, "worker", result.Name)
	assert.True(t, mockWT.created, "worktree should have been created")
	assert.True(t, mockWT.removed, "clean worktree should have been removed")
}

func TestDefaultSubagentSpawnerWorktreeIsolation_PreserveDirty(t *testing.T) {
	recorder := &recordingProvider{}
	mockWT := &mockWorktreeProvider{dir: t.TempDir(), hasChanges: true}
	spawner := &DefaultSubagentSpawner{
		Provider:         recorder,
		ParentTools:      tools.NewRegistry(),
		Config:           &config.Config{Provider: config.ProviderConfig{Model: "test"}},
		WorktreeProvider: mockWT,
	}
	result, err := spawner.Spawn(context.Background(), SubagentConfig{
		Name:      "worker",
		Isolation: "worktree",
	}, "hello")
	require.NoError(t, err)
	assert.Equal(t, "worker", result.Name)
	assert.True(t, mockWT.created, "worktree should have been created")
	assert.False(t, mockWT.removed, "dirty worktree should NOT have been removed")
}

// mockWorktreeProvider implements WorktreeProvider for testing.
type mockWorktreeProvider struct {
	dir        string
	hasChanges bool
	created    bool
	removed    bool
}

func (m *mockWorktreeProvider) CreateWorktree(_ context.Context, _ string) (*WorktreeHandle, error) {
	m.created = true
	return &WorktreeHandle{Dir: m.dir, Name: "test-wt"}, nil
}

func (m *mockWorktreeProvider) HasWorktreeChanges(_ context.Context, _ string) (bool, error) {
	return m.hasChanges, nil
}

func (m *mockWorktreeProvider) RemoveWorktree(_ context.Context, _ string) error {
	m.removed = true
	return nil
}

type recordingProvider struct {
	system string
}

func (p *recordingProvider) Stream(_ context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	p.system = req.System
	ch := make(chan provider.StreamEvent, 2)
	ch <- provider.StreamEvent{Type: "text_delta", Text: "done"}
	ch <- provider.StreamEvent{Type: "done", InputTokens: 1, OutputTokens: 1}
	close(ch)
	return ch, nil
}

func TestSpawnParallelEmptyRequests(t *testing.T) {
	spawner := &DefaultSubagentSpawner{
		Config: &config.Config{},
	}
	results, err := spawner.SpawnParallel(context.Background(), nil, 3)
	assert.NoError(t, err)
	assert.Empty(t, results)
}

func TestSpawnParallelErrorPropagation(t *testing.T) {
	spawner := &DefaultSubagentSpawner{
		Config: &config.Config{
			Provider: config.ProviderConfig{Model: "test"},
			Agent:    config.AgentConfig{MaxTurns: 5, ContextBudget: 100000},
		},
	}

	requests := []SubagentRequest{
		{Config: SubagentConfig{Name: "a"}, Prompt: "task a"},
		{Config: SubagentConfig{Name: "b"}, Prompt: "task b"},
	}

	// Without a provider, Spawn will fail — SpawnParallel should collect errors
	results, err := spawner.SpawnParallel(context.Background(), requests, 2)
	assert.NoError(t, err) // top-level error is nil
	assert.Len(t, results, 2)
	assert.Error(t, results[0].Error)
	assert.Error(t, results[1].Error)
}

// TestSpawnDispatchesWorktreeCreateAndRemove asserts that the spawner
// fires HookOnWorktreeCreate before asking the provider to create a
// worktree and HookOnWorktreeRemove before removing a clean worktree.
func TestSpawnDispatchesWorktreeCreateAndRemove(t *testing.T) {
	var createData, removeData map[string]any

	backendHooks := map[skills.HookPhase]skills.HookHandler{
		skills.HookOnWorktreeCreate: func(event skills.HookEvent) (skills.HookResult, error) {
			createData = event.Data
			return skills.HookResult{}, nil
		},
		skills.HookOnWorktreeRemove: func(event skills.HookEvent) (skills.HookResult, error) {
			removeData = event.Data
			return skills.HookResult{}, nil
		},
	}

	parentRuntime := makeTestRuntime(t, "worktree-hook-skill", toolManifest("worktree-hook-skill"), nil, backendHooks)
	mockWT := &mockWorktreeProvider{dir: t.TempDir()}
	spawner := &DefaultSubagentSpawner{
		Provider:           &recordingProvider{},
		ParentTools:        tools.NewRegistry(),
		ParentSkillRuntime: parentRuntime,
		Config:             &config.Config{Provider: config.ProviderConfig{Model: "test"}},
		WorktreeProvider:   mockWT,
	}

	_, err := spawner.Spawn(context.Background(), SubagentConfig{
		Name:      "wt-hook-worker",
		Isolation: "worktree",
	}, "go")
	require.NoError(t, err)

	require.NotNil(t, createData, "HookOnWorktreeCreate should fire before worktree is created")
	assert.Equal(t, "wt-hook-worker", createData[skills.HookDataSubagentName])
	assert.NotEmpty(t, createData[skills.HookDataWorktreeName])

	require.NotNil(t, removeData, "HookOnWorktreeRemove should fire before clean worktree is removed")
	assert.Equal(t, "wt-hook-worker", removeData[skills.HookDataSubagentName])
}

// TestSpawnDispatchesTaskCreatedAndCompleted asserts that the spawner
// fires HookOnTaskCreated before running the child and HookOnTaskCompleted
// after the child returns, dispatching on the parent runtime so parent
// hooks observe the task lifecycle.
func TestSpawnDispatchesTaskCreatedAndCompleted(t *testing.T) {
	var createdData, completedData map[string]any

	backendHooks := map[skills.HookPhase]skills.HookHandler{
		skills.HookOnTaskCreated: func(event skills.HookEvent) (skills.HookResult, error) {
			createdData = event.Data
			return skills.HookResult{}, nil
		},
		skills.HookOnTaskCompleted: func(event skills.HookEvent) (skills.HookResult, error) {
			completedData = event.Data
			return skills.HookResult{}, nil
		},
	}

	parentRuntime := makeTestRuntime(t, "task-hook-skill", toolManifest("task-hook-skill"), nil, backendHooks)
	spawner := &DefaultSubagentSpawner{
		Provider:           &recordingProvider{},
		ParentTools:        tools.NewRegistry(),
		ParentSkillRuntime: parentRuntime,
		Config:             &config.Config{Provider: config.ProviderConfig{Model: "test"}},
	}

	result, err := spawner.Spawn(context.Background(), SubagentConfig{
		Name: "hook-observed",
	}, "do the thing")
	require.NoError(t, err)
	require.NotNil(t, result)

	require.NotNil(t, createdData, "HookOnTaskCreated should fire before child runs")
	assert.Equal(t, "hook-observed", createdData[skills.HookDataName])
	assert.Equal(t, "do the thing", createdData[skills.HookDataPrompt])

	require.NotNil(t, completedData, "HookOnTaskCompleted should fire after child returns")
	assert.Equal(t, "hook-observed", completedData[skills.HookDataName])
	assert.NotNil(t, completedData[skills.HookDataOutput])
}

func TestDefaultSubagentSpawnerSkillSnapshotFiltering(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	loader := skills.NewLoader("", "")
	loader.RegisterBuiltin(&skills.SkillManifest{
		Name:        "alpha-skill",
		Version:     "1.0.0",
		Description: "Alpha",
		Types:       []skills.SkillType{skills.SkillTypePrompt},
		Prompt:      skills.PromptConfig{SystemPromptFile: "Alpha guidance."},
	})
	loader.RegisterBuiltin(&skills.SkillManifest{
		Name:        "beta-skill",
		Version:     "1.0.0",
		Description: "Beta",
		Types:       []skills.SkillType{skills.SkillTypePrompt},
		Prompt:      skills.PromptConfig{SystemPromptFile: "Beta guidance."},
	})
	parentRuntime := skills.NewRuntime(loader, s, tools.NewRegistry(), nil,
		func(manifest skills.SkillManifest, dir string) (skills.SkillBackend, error) {
			return &skillMockBackend{}, nil
		},
		func(skillName string, declared []skills.Permission) skills.PermissionChecker {
			return &skillMockChecker{}
		},
	)
	require.NoError(t, parentRuntime.Discover(nil))
	require.NoError(t, parentRuntime.Activate("alpha-skill"))
	require.NoError(t, parentRuntime.Activate("beta-skill"))

	recorder := &recordingProvider{}
	spawner := &DefaultSubagentSpawner{
		Provider:           recorder,
		ParentTools:        tools.NewRegistry(),
		ParentSkillRuntime: parentRuntime,
		Config:             &config.Config{Provider: config.ProviderConfig{Model: "test"}},
	}

	inheritFalse := false
	_, err = spawner.Spawn(context.Background(), SubagentConfig{
		Name:          "focused",
		InheritSkills: &inheritFalse,
		ExtraSkills:   []string{"beta-skill"},
	}, "hello")
	require.NoError(t, err)
	assert.Contains(t, recorder.system, "Beta guidance.")
	assert.NotContains(t, recorder.system, "Alpha guidance.")

	_, err = spawner.Spawn(context.Background(), SubagentConfig{
		Name:          "trimmed",
		DisableSkills: []string{"beta-skill"},
	}, "hello")
	require.NoError(t, err)
	assert.Contains(t, recorder.system, "Alpha guidance.")
	assert.NotContains(t, recorder.system, "Beta guidance.")
}
