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
