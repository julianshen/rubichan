package provider_test

import (
	"testing"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Import sub-packages to trigger init() registration
	_ "github.com/julianshen/rubichan/internal/provider/anthropic"
	_ "github.com/julianshen/rubichan/internal/provider/openai"
)

func TestNewProviderAnthropic(t *testing.T) {
	// Set the ANTHROPIC_API_KEY env var for this test
	t.Setenv("ANTHROPIC_API_KEY", "test-anthropic-key")

	cfg := config.DefaultConfig()
	cfg.Provider.Default = "anthropic"

	p, err := provider.NewProvider(cfg)
	require.NoError(t, err)
	assert.NotNil(t, p)
}

func TestNewProviderAnthropicMissingKey(t *testing.T) {
	// Ensure env var is not set (t.Setenv restores original value after test)
	t.Setenv("ANTHROPIC_API_KEY", "")

	cfg := config.DefaultConfig()
	cfg.Provider.Default = "anthropic"
	cfg.Provider.Anthropic.APIKeySource = "env"

	_, err := provider.NewProvider(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ANTHROPIC_API_KEY")
}

func TestNewProviderOpenAI(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")

	cfg := config.DefaultConfig()
	cfg.Provider.Default = "openai"
	cfg.Provider.OpenAI = []config.OpenAICompatibleConfig{
		{
			Name:         "openai",
			BaseURL:      "https://api.openai.com/v1",
			APIKeySource: "env",
		},
	}

	p, err := provider.NewProvider(cfg)
	require.NoError(t, err)
	assert.NotNil(t, p)
}

func TestNewProviderOpenRouter(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "test-openrouter-key")

	cfg := config.DefaultConfig()
	cfg.Provider.Default = "openrouter"
	cfg.Provider.OpenAI = []config.OpenAICompatibleConfig{
		{
			Name:         "openrouter",
			BaseURL:      "https://openrouter.ai/api/v1",
			APIKeySource: "env",
			ExtraHeaders: map[string]string{
				"HTTP-Referer": "https://myapp.com",
			},
		},
	}

	p, err := provider.NewProvider(cfg)
	require.NoError(t, err)
	assert.NotNil(t, p)
}

func TestNewProviderOpenAIWithConfigKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "openai"
	cfg.Provider.OpenAI = []config.OpenAICompatibleConfig{
		{
			Name:         "openai",
			BaseURL:      "https://api.openai.com/v1",
			APIKeySource: "config",
			APIKey:       "sk-from-config",
		},
	}

	p, err := provider.NewProvider(cfg)
	require.NoError(t, err)
	assert.NotNil(t, p)
}

func TestNewProviderOpenAIMissingKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")

	cfg := config.DefaultConfig()
	cfg.Provider.Default = "openai"
	cfg.Provider.OpenAI = []config.OpenAICompatibleConfig{
		{
			Name:         "openai",
			BaseURL:      "https://api.openai.com/v1",
			APIKeySource: "env",
		},
	}

	_, err := provider.NewProvider(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OPENAI_API_KEY")
}

func TestNewProviderUnknown(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "unknown-provider"

	_, err := provider.NewProvider(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider")
}
