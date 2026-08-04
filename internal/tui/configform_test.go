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

func TestConfigFormSaveOpenAIBaseURLOnlyEditWithBlankKeyPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "openai"
	cfg.Provider.OpenAI = []config.OpenAICompatibleConfig{
		{Name: "openai", BaseURL: "https://api.openai.com/v1", APIKeySource: "env"},
	}

	form := NewConfigForm(cfg, path)
	require.Empty(t, form.openaiKey, "env-sourced key should not be pre-filled into the form")
	form.openaiBaseURL = "https://custom-proxy.example.com/v1"
	require.NoError(t, form.Save())

	loaded, err := config.Load(path)
	require.NoError(t, err)
	require.Len(t, loaded.Provider.OpenAI, 1)
	assert.Equal(t, "https://custom-proxy.example.com/v1", loaded.Provider.OpenAI[0].BaseURL)
	assert.Equal(t, "env", loaded.Provider.OpenAI[0].APIKeySource, "must not be downgraded to config when no key was entered")
	assert.Empty(t, loaded.Provider.OpenAI[0].APIKey)
}

func TestConfigFormSaveClearingConfigSourcedAnthropicKeyResetsSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "anthropic"
	// Simulate an existing on-disk config with a previously-stored key —
	// APIKeySource must be captured at NewConfigForm construction time,
	// before the field is cleared below, to distinguish this from a key
	// that was always env-sourced (see the sibling test).
	cfg.Provider.Anthropic.APIKeySource = "config"
	cfg.Provider.Anthropic.APIKey = "sk-ant-old"

	form := NewConfigForm(cfg, path)
	// Simulate the user clearing the field in the UI.
	cfg.Provider.Anthropic.APIKey = ""
	require.NoError(t, form.Save())

	loaded, err := config.Load(path)
	require.NoError(t, err)
	assert.Empty(t, loaded.Provider.Anthropic.APIKey)
	assert.Equal(t, "env", loaded.Provider.Anthropic.APIKeySource, "clearing a config-sourced key must reset APIKeySource, not leave it pointing at an empty value")
}

func TestConfigFormSaveClearingConfigSourcedZaiKeyResetsSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "zai"
	cfg.Provider.Zai.APIKeySource = "config"
	cfg.Provider.Zai.APIKey = "zai-old-key"

	form := NewConfigForm(cfg, path)
	cfg.Provider.Zai.APIKey = ""
	require.NoError(t, form.Save())

	loaded, err := config.Load(path)
	require.NoError(t, err)
	assert.Empty(t, loaded.Provider.Zai.APIKey)
	assert.Equal(t, "env", loaded.Provider.Zai.APIKeySource, "clearing a config-sourced key must reset APIKeySource, not leave it pointing at an empty value")
}

func TestConfigFormSaveLeavesEnvSourcedAnthropicKeyUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "anthropic"
	cfg.Provider.Anthropic.APIKeySource = "env"
	cfg.Provider.Anthropic.APIKey = ""

	form := NewConfigForm(cfg, path)
	// Field is never touched — blank means "use the env var", not "clear it".
	require.NoError(t, form.Save())

	loaded, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "env", loaded.Provider.Anthropic.APIKeySource, "an untouched env-sourced key must not be reset")
}

func TestConfigFormSaveLeavesEnvSourcedZaiKeyUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "zai"
	cfg.Provider.Zai.APIKeySource = "env"
	cfg.Provider.Zai.APIKey = ""

	form := NewConfigForm(cfg, path)
	require.NoError(t, form.Save())

	loaded, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "env", loaded.Provider.Zai.APIKeySource, "an untouched env-sourced key must not be reset")
}

func TestConfigFormSaveClearingKeyThenSwitchingProviderStillResetsSource(t *testing.T) {
	// Reproduces a realistic multi-step edit: the user clears a
	// config-sourced Anthropic key, then navigates back to the provider
	// group and switches the selection to Z.ai before finishing the form.
	// Provider.Default at Save() time is "zai", not "anthropic" — the
	// reconciliation must still apply to Anthropic's now-stale
	// APIKeySource, since it isn't gated on which provider ends up
	// selected.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "anthropic"
	cfg.Provider.Anthropic.APIKeySource = "config"
	cfg.Provider.Anthropic.APIKey = "sk-ant-old"

	form := NewConfigForm(cfg, path)
	cfg.Provider.Anthropic.APIKey = "" // cleared while still on the Anthropic group
	cfg.Provider.Default = "zai"       // then switched away before finishing
	require.NoError(t, form.Save())

	loaded, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "zai", loaded.Provider.Default)
	assert.Empty(t, loaded.Provider.Anthropic.APIKey)
	assert.Equal(t, "env", loaded.Provider.Anthropic.APIKeySource, "clearing a key must reset its APIKeySource even if a different provider ends up selected by the time the form completes")
}

func TestConfigFormSaveOpenAIPreservesExtraHeadersOnUpdate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "openai"
	cfg.Provider.OpenAI = []config.OpenAICompatibleConfig{
		{Name: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "sk-old", ExtraHeaders: map[string]string{"X-Custom": "value"}},
	}

	form := NewConfigForm(cfg, path)
	form.openaiKey = "sk-new"
	require.NoError(t, form.Save())

	loaded, err := config.Load(path)
	require.NoError(t, err)
	require.Len(t, loaded.Provider.OpenAI, 1)
	assert.Equal(t, "sk-new", loaded.Provider.OpenAI[0].APIKey)
	assert.Equal(t, map[string]string{"X-Custom": "value"}, loaded.Provider.OpenAI[0].ExtraHeaders, "must not be silently wiped by an unrelated field update")
}
