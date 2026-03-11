package agentsdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSubagentConfigDefaults(t *testing.T) {
	cfg := SubagentConfig{Name: "test"}
	assert.Equal(t, "test", cfg.Name)
	assert.Equal(t, 0, cfg.MaxTurns) // caller sets default
	assert.Equal(t, 0, cfg.MaxDepth) // caller sets default
	assert.Nil(t, cfg.InheritSkills) // nil = inherit
	assert.Empty(t, cfg.Isolation)
}

func TestSubagentConfigWorktreeIsolation(t *testing.T) {
	cfg := SubagentConfig{
		Name:      "isolated",
		Isolation: "worktree",
		MaxTurns:  5,
	}
	assert.Equal(t, "worktree", cfg.Isolation)
}

func TestSubagentResultFields(t *testing.T) {
	r := SubagentResult{
		Name:         "explorer",
		Output:       "done",
		ToolsUsed:    []string{"file", "shell"},
		TurnCount:    3,
		InputTokens:  500,
		OutputTokens: 200,
	}
	assert.Equal(t, "explorer", r.Name)
	assert.Len(t, r.ToolsUsed, 2)
	assert.NoError(t, r.Error)
}

func TestMemoryEntryFields(t *testing.T) {
	e := MemoryEntry{Tag: "gotcha", Content: "use snake_case"}
	assert.Equal(t, "gotcha", e.Tag)
	assert.Equal(t, "use snake_case", e.Content)
}
