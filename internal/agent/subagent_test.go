package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
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
