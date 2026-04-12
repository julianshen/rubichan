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

func TestHasUsableCredentialsForProviderZaiConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Provider.Zai.APIKeySource = "config"
	cfg.Provider.Zai.APIKey = "zai-test-key"

	assert.True(t, HasUsableCredentialsForProvider(cfg, "zai"))
}

func TestHasUsableCredentialsForProviderZaiEnv(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Provider.Zai.APIKeySource = "env"

	assert.False(t, HasUsableCredentialsForProvider(cfg, "zai"))

	t.Setenv("Z_AI_API_KEY", "zai-env-key")
	assert.True(t, HasUsableCredentialsForProvider(cfg, "zai"))
}

func TestHasUsableCredentialsNilConfig(t *testing.T) {
	t.Parallel()

	assert.False(t, HasUsableCredentialsForProvider(nil, "anthropic"))
}

func TestHasUsableCredentialsForDefaultProviderNilConfig(t *testing.T) {
	t.Parallel()

	assert.False(t, HasUsableCredentialsForDefaultProvider(nil))
}

func TestHasUsableCredentialsForProviderOllama(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	// Ollama always returns true (no API key needed).
	assert.True(t, HasUsableCredentialsForProvider(cfg, "ollama"))
}

func TestHasUsableCredentialsForProviderUnknown(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	assert.False(t, HasUsableCredentialsForProvider(cfg, "nonexistent-provider"))
}

func TestHasUsableCredentialsForProviderOpenAINoMatch(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Provider.OpenAI = []OpenAICompatibleConfig{
		{Name: "openai", APIKeySource: "config", APIKey: "sk-test"},
	}
	// Looking for "openrouter" should not match "openai".
	assert.False(t, HasUsableCredentialsForProvider(cfg, "openrouter"))
}

func TestResolveAPIKeyKeyringFallback(t *testing.T) {
	// "keyring" source falls back to env lookup.
	t.Setenv("TEST_KEYRING_KEY", "sk-keyring")
	key, err := ResolveAPIKey("keyring", "", "TEST_KEYRING_KEY")
	require.NoError(t, err)
	assert.Equal(t, "sk-keyring", key)
}

func TestResolveFromEnvEmptyVarName(t *testing.T) {
	t.Parallel()

	_, err := ResolveAPIKey("env", "", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no environment variable name specified")
}

func TestOpenAICompatibleEnvVarSpecialCharsOnly(t *testing.T) {
	t.Parallel()

	// A name with only special characters normalizes to empty.
	assert.Equal(t, "", OpenAICompatibleEnvVar("---"))
}

func TestOpenAICompatibleEnvVarConsecutiveSpecialChars(t *testing.T) {
	t.Parallel()

	// Consecutive special chars should not produce consecutive underscores.
	result := OpenAICompatibleEnvVar("my..provider")
	assert.Equal(t, "MY_PROVIDER_API_KEY", result)
}
