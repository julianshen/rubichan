package agentsdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultAgentConfig(t *testing.T) {
	cfg := DefaultAgentConfig()

	assert.Equal(t, "claude-sonnet-4-5", cfg.Model)
	assert.Equal(t, 50, cfg.MaxTurns)
	assert.Equal(t, 100000, cfg.ContextBudget)
	assert.Equal(t, 4096, cfg.MaxOutputTokens)
	assert.InDelta(t, 0.95, cfg.CompactTrigger, 0.001)
	assert.InDelta(t, 0.98, cfg.HardBlock, 0.001)
	assert.Equal(t, 4096, cfg.ResultOffloadThreshold)
	assert.InDelta(t, 0.10, cfg.ToolDeferralThreshold, 0.001)
	assert.Empty(t, cfg.SystemPrompt)
}

func TestAgentConfigCustomValues(t *testing.T) {
	cfg := AgentConfig{
		Model:         "gpt-4o",
		MaxTurns:      10,
		ContextBudget: 50000,
		SystemPrompt:  "You are a code reviewer.",
	}

	assert.Equal(t, "gpt-4o", cfg.Model)
	assert.Equal(t, 10, cfg.MaxTurns)
	assert.Equal(t, "You are a code reviewer.", cfg.SystemPrompt)
}
