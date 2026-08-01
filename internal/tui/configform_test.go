package tui

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/config"
)

func TestConfigFormCreation(t *testing.T) {
	cfg := config.DefaultConfig()
	form := NewConfigForm(cfg, "/tmp/test-config.toml")
	assert.NotNil(t, form)
	assert.NotNil(t, form.Form())
}

func TestConfigFormGroupCount(t *testing.T) {
	cfg := config.DefaultConfig()
	form := NewConfigForm(cfg, "/tmp/test-config.toml")
	assert.Equal(t, 8, form.GroupCount())
}

func TestConfigFormIsCompletedAborted(t *testing.T) {
	cfg := config.DefaultConfig()
	form := NewConfigForm(cfg, "/tmp/test-config.toml")

	assert.False(t, form.IsCompleted())
	assert.False(t, form.IsAborted())
}

func TestConfigFormSave(t *testing.T) {
	for _, provider := range []string{"ollama", "zai"} {
		t.Run(provider, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.toml")
			cfg := config.DefaultConfig()
			cfg.Provider.Default = provider

			form := NewConfigForm(cfg, path)
			require.NoError(t, form.Save())

			loaded, err := config.Load(path)
			require.NoError(t, err)
			assert.Equal(t, provider, loaded.Provider.Default)
		})
	}
}

func TestConfigFormSaveWritesAnthropicKeyToAnthropicField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "anthropic"
	cfg.Provider.Anthropic.APIKey = "sk-ant-test"

	form := NewConfigForm(cfg, path)
	require.NoError(t, form.Save())

	loaded, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "sk-ant-test", loaded.Provider.Anthropic.APIKey)
	assert.Equal(t, "config", loaded.Provider.Anthropic.APIKeySource)
}

func TestConfigFormSaveWritesZaiKeyToZaiFieldNotAnthropic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "zai"
	cfg.Provider.Zai.APIKey = "zai-test-key"

	form := NewConfigForm(cfg, path)
	require.NoError(t, form.Save())

	loaded, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "zai-test-key", loaded.Provider.Zai.APIKey)
	assert.Equal(t, "config", loaded.Provider.Zai.APIKeySource)
	assert.Empty(t, loaded.Provider.Anthropic.APIKey, "zai key must not leak into the anthropic field")
}

func TestConfigFormSaveZaiBaseURLOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "zai"
	cfg.Provider.Zai.BaseURL = "https://custom.zai.example.com"

	form := NewConfigForm(cfg, path)
	require.NoError(t, form.Save())

	loaded, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "https://custom.zai.example.com", loaded.Provider.Zai.BaseURL)
}

func TestConfigFormSaveOllamaBaseURLOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "ollama"
	cfg.Provider.Ollama.BaseURL = "http://custom-ollama:1234"

	form := NewConfigForm(cfg, path)
	require.NoError(t, form.Save())

	loaded, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "http://custom-ollama:1234", loaded.Provider.Ollama.BaseURL)
}

func TestConfigFormSaveOpenAINewEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "openai"

	form := NewConfigForm(cfg, path)
	form.openaiKey = "sk-openai-test"
	form.openaiBaseURL = "https://openrouter.ai/api/v1"
	require.NoError(t, form.Save())

	loaded, err := config.Load(path)
	require.NoError(t, err)
	require.Len(t, loaded.Provider.OpenAI, 1)
	assert.Equal(t, "openai", loaded.Provider.OpenAI[0].Name)
	assert.Equal(t, "https://openrouter.ai/api/v1", loaded.Provider.OpenAI[0].BaseURL)
	assert.Equal(t, "sk-openai-test", loaded.Provider.OpenAI[0].APIKey)
}

func TestConfigFormSaveOpenAIUpdatesExistingEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "openai"
	// Simulate editing a config that already has an "openai" entry (unlike
	// bootstrap, which always starts from an empty slice) — /config must
	// update it in place, not append a duplicate.
	cfg.Provider.OpenAI = []config.OpenAICompatibleConfig{
		{Name: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "sk-old"},
		{Name: "openrouter", BaseURL: "https://openrouter.ai/api/v1", APIKey: "sk-other"},
	}

	form := NewConfigForm(cfg, path)
	form.openaiKey = "sk-new"
	require.NoError(t, form.Save())

	loaded, err := config.Load(path)
	require.NoError(t, err)
	require.Len(t, loaded.Provider.OpenAI, 2, "must update the existing openai entry, not append a duplicate")
	assert.Equal(t, "sk-new", loaded.Provider.OpenAI[0].APIKey)
	assert.Equal(t, "sk-other", loaded.Provider.OpenAI[1].APIKey, "the unrelated openrouter entry must be untouched")
}
