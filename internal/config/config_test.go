package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, "anthropic", cfg.Provider.Default)
	assert.Equal(t, "claude-sonnet-4-5", cfg.Provider.Model)
	assert.Equal(t, 50, cfg.Agent.MaxTurns)
	assert.Equal(t, "prompt", cfg.Agent.ApprovalMode)
	assert.Equal(t, 100000, cfg.Agent.ContextBudget)
}

func TestLoadFromFile(t *testing.T) {
	tomlContent := `
[provider]
default = "openai"
model = "gpt-4o"

[provider.anthropic]
api_key_source = "keyring"

[agent]
max_turns = 30
approval_mode = "auto"
context_budget = 50000
`
	tmpFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(tmpFile, []byte(tomlContent), 0644))

	cfg, err := Load(tmpFile)
	require.NoError(t, err)
	assert.Equal(t, "openai", cfg.Provider.Default)
	assert.Equal(t, "gpt-4o", cfg.Provider.Model)
	assert.Equal(t, "keyring", cfg.Provider.Anthropic.APIKeySource)
	assert.Equal(t, 30, cfg.Agent.MaxTurns)
	assert.Equal(t, "auto", cfg.Agent.ApprovalMode)
	assert.Equal(t, 50000, cfg.Agent.ContextBudget)
}
