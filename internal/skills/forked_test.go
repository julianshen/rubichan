package skills

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockSubagentSpawner struct {
	spawnCalled bool
	lastCfg     SubagentConfig
	lastPrompt  string
	result      *SubagentResult
	spawnErr    error
}

func (m *mockSubagentSpawner) Spawn(ctx context.Context, cfg SubagentConfig, prompt string) (*SubagentResult, error) {
	m.spawnCalled = true
	m.lastCfg = cfg
	m.lastPrompt = prompt
	result := m.result
	if result == nil {
		result = &SubagentResult{Name: cfg.Name, Output: "mock output"}
	}
	return result, m.spawnErr
}

func TestForkedSkillExecutorRequiresSpawner(t *testing.T) {
	_, err := NewForkedSkillExecutor(ForkedSkillExecutorConfig{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "spawner required")
}

func TestForkedSkillExecutorInlineSkill(t *testing.T) {
	cfg := ForkedSkillExecutorConfig{
		Spawner: &mockSubagentSpawner{},
	}
	exec, err := NewForkedSkillExecutor(cfg)
	require.NoError(t, err)

	// Inline skill should return nil, false (not handled).
	result, handled, err := exec.Execute(context.Background(), &Skill{
		Manifest: &SkillManifest{Name: "inline-skill", ExecutionMode: ExecutionModeInline},
	}, "test prompt")
	assert.NoError(t, err)
	assert.False(t, handled)
	assert.Nil(t, result)
}

func TestForkedSkillExecutorForkedSkill(t *testing.T) {
	spawner := &mockSubagentSpawner{}
	cfg := ForkedSkillExecutorConfig{
		Spawner:         spawner,
		DefaultMaxTurns: 5,
		DefaultMaxDepth: 2,
	}
	exec, err := NewForkedSkillExecutor(cfg)
	require.NoError(t, err)

	result, handled, err := exec.Execute(context.Background(), &Skill{
		Manifest:        &SkillManifest{Name: "forked-skill", ExecutionMode: ExecutionModeFork},
		InstructionBody: "You are a helpful assistant.",
	}, "test prompt")
	require.NoError(t, err)
	assert.True(t, handled)
	assert.NotNil(t, result)
	assert.Equal(t, "forked-skill", result.Name)
	assert.Equal(t, "mock output", result.Output)

	// Verify spawner was called with correct config.
	assert.True(t, spawner.spawnCalled)
	assert.Equal(t, "forked-skill", spawner.lastCfg.Name)
	assert.Equal(t, 5, spawner.lastCfg.MaxTurns)
	assert.Equal(t, 2, spawner.lastCfg.MaxDepth)
	assert.Equal(t, 1, spawner.lastCfg.Depth)
	assert.Equal(t, "You are a helpful assistant.", spawner.lastCfg.SystemPrompt)
	assert.Equal(t, "test prompt", spawner.lastPrompt)
}

func TestForkedSkillExecutorSpawnError(t *testing.T) {
	spawner := &mockSubagentSpawner{spawnErr: assert.AnError}
	cfg := ForkedSkillExecutorConfig{
		Spawner: spawner,
	}
	exec, err := NewForkedSkillExecutor(cfg)
	require.NoError(t, err)

	_, handled, err := exec.Execute(context.Background(), &Skill{
		Manifest: &SkillManifest{Name: "forked-skill", ExecutionMode: ExecutionModeFork},
	}, "test prompt")
	assert.Error(t, err)
	assert.True(t, handled)
	assert.Contains(t, err.Error(), "forked skill \"forked-skill\"")
}

func TestForkedSkillExecutorNilSkill(t *testing.T) {
	cfg := ForkedSkillExecutorConfig{
		Spawner: &mockSubagentSpawner{},
	}
	exec, err := NewForkedSkillExecutor(cfg)
	require.NoError(t, err)

	result, handled, err := exec.Execute(context.Background(), nil, "test")
	assert.NoError(t, err)
	assert.False(t, handled)
	assert.Nil(t, result)
}

func TestForkedSkillExecutorNilManifest(t *testing.T) {
	cfg := ForkedSkillExecutorConfig{
		Spawner: &mockSubagentSpawner{},
	}
	exec, err := NewForkedSkillExecutor(cfg)
	require.NoError(t, err)

	result, handled, err := exec.Execute(context.Background(), &Skill{Manifest: nil}, "test")
	assert.NoError(t, err)
	assert.False(t, handled)
	assert.Nil(t, result)
}

func TestForkedSkillExecutorDefaults(t *testing.T) {
	spawner := &mockSubagentSpawner{}
	cfg := ForkedSkillExecutorConfig{
		Spawner: spawner,
	}
	exec, err := NewForkedSkillExecutor(cfg)
	require.NoError(t, err)

	_, _, err = exec.Execute(context.Background(), &Skill{
		Manifest: &SkillManifest{Name: "forked-skill", ExecutionMode: ExecutionModeFork},
	}, "test")
	require.NoError(t, err)

	// Should use defaults (10 turns, 3 depth).
	assert.Equal(t, 10, spawner.lastCfg.MaxTurns)
	assert.Equal(t, 3, spawner.lastCfg.MaxDepth)
}
