package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, "anthropic", cfg.Provider.Default)
	assert.Equal(t, "claude-sonnet-4-5", cfg.Provider.Model)
	assert.Equal(t, 50, cfg.Agent.MaxTurns)
	assert.Equal(t, "prompt", cfg.Agent.ApprovalMode)
	assert.Equal(t, 100000, cfg.Agent.ContextBudget)
}
