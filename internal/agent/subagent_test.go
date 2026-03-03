package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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

func TestSubagentSpawnerInterface(t *testing.T) {
	// Verify the interface is well-formed by checking it compiles.
	// DefaultSubagentSpawner (added in the next commit) will implement it.
	var _ SubagentSpawner = (SubagentSpawner)(nil)
}
