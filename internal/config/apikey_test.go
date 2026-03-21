package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveAPIKeyFromEnv(t *testing.T) {
	t.Setenv("TEST_API_KEY", "sk-test-12345")
	key, err := ResolveAPIKey("env", "", "TEST_API_KEY")
	require.NoError(t, err)
	assert.Equal(t, "sk-test-12345", key)
}

func TestResolveAPIKeyFromConfig(t *testing.T) {
	t.Parallel()

	key, err := ResolveAPIKey("config", "sk-from-config", "")
	require.NoError(t, err)
	assert.Equal(t, "sk-from-config", key)
}

func TestResolveAPIKeyMissingEnvVar(t *testing.T) {
	t.Parallel()

	_, err := ResolveAPIKey("env", "", "NONEXISTENT_KEY_VAR")
	assert.Error(t, err)
}

func TestResolveAPIKeyEmptyConfig(t *testing.T) {
	t.Parallel()

	_, err := ResolveAPIKey("config", "", "")
	assert.Error(t, err)
}

func TestResolveAPIKeyUnknownSource(t *testing.T) {
	t.Parallel()

	_, err := ResolveAPIKey("unknown", "", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown api_key_source")
}

func TestOpenAICompatibleEnvVar(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "OPENROUTER_API_KEY", OpenAICompatibleEnvVar("openrouter"))
	assert.Equal(t, "AZURE_OPENAI_API_KEY", OpenAICompatibleEnvVar("azure-openai"))
	assert.Equal(t, "MY_PROVIDER_01_API_KEY", OpenAICompatibleEnvVar(" my.provider 01 "))
	assert.Equal(t, "", OpenAICompatibleEnvVar(""))
}

func TestResolveOpenAICompatibleAPIKey(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-openrouter")
	key, err := ResolveOpenAICompatibleAPIKey(OpenAICompatibleConfig{
		Name:         "openrouter",
		APIKeySource: "env",
	})
	require.NoError(t, err)
	assert.Equal(t, "sk-openrouter", key)
}

func TestHasUsableCredentialsForDefaultProvider(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Provider.Default = "openrouter"
	cfg.Provider.OpenAI = []OpenAICompatibleConfig{
		{Name: "openrouter", APIKeySource: "env"},
	}

	assert.False(t, HasUsableCredentialsForDefaultProvider(cfg))

	t.Setenv("OPENROUTER_API_KEY", "sk-openrouter")
	assert.True(t, HasUsableCredentialsForDefaultProvider(cfg))
}

func TestHasUsableCredentialsForProviderAnthropicConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Provider.Default = "anthropic"
	cfg.Provider.Anthropic.APIKeySource = "config"
	cfg.Provider.Anthropic.APIKey = "sk-ant"

	assert.True(t, HasUsableCredentialsForProvider(cfg, "anthropic"))
}
