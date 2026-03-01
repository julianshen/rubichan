package tui

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	_ "github.com/julianshen/rubichan/internal/provider/openai"
)

func TestBootstrapFormCreation(t *testing.T) {
	form := NewBootstrapForm("/tmp/test-config.toml")
	assert.NotNil(t, form)
	assert.NotNil(t, form.Form())
	assert.NotNil(t, form.Config())
}

func TestBootstrapFormSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	form := NewBootstrapForm(path)
	form.Config().Provider.Default = "anthropic"
	form.Config().Provider.Anthropic.APIKey = "sk-test-key"

	err := form.Save()
	require.NoError(t, err)

	// Verify saved config can be loaded back.
	loaded, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "anthropic", loaded.Provider.Default)
	assert.Equal(t, "sk-test-key", loaded.Provider.Anthropic.APIKey)
}

func TestNeedsBootstrapNoConfigFile(t *testing.T) {
	// Non-existent path → needs bootstrap (config.Load returns default with no API key).
	assert.True(t, NeedsBootstrap("/nonexistent/path/config.toml"))
}

func TestNeedsBootstrapWithAnthropicKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := config.DefaultConfig()
	cfg.Provider.Anthropic.APIKey = "sk-test"
	require.NoError(t, config.Save(path, cfg))

	assert.False(t, NeedsBootstrap(path))
}

func TestNeedsBootstrapWithEnvKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := config.DefaultConfig()
	require.NoError(t, config.Save(path, cfg))

	// Set env var.
	t.Setenv("ANTHROPIC_API_KEY", "sk-env-test")
	assert.False(t, NeedsBootstrap(path))
}

func TestNeedsBootstrapWithOllama(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := config.DefaultConfig()
	cfg.Provider.Default = "ollama"
	require.NoError(t, config.Save(path, cfg))

	assert.False(t, NeedsBootstrap(path))
}

func TestNeedsBootstrapWithOpenAIKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := config.DefaultConfig()
	cfg.Provider.OpenAI = []config.OpenAICompatibleConfig{
		{Name: "test", APIKey: "sk-openai-test"},
	}
	require.NoError(t, config.Save(path, cfg))

	assert.False(t, NeedsBootstrap(path))
}

func TestNeedsBootstrapWithOpenAIEnvKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := config.DefaultConfig()
	cfg.Provider.OpenAI = []config.OpenAICompatibleConfig{
		{Name: "test", APIKeySource: "MY_OPENAI_KEY"},
	}
	require.NoError(t, config.Save(path, cfg))

	t.Setenv("MY_OPENAI_KEY", "sk-from-env")
	assert.False(t, NeedsBootstrap(path))
}

func TestBootstrapFormSetForm(t *testing.T) {
	form := NewBootstrapForm("/tmp/test.toml")
	originalForm := form.Form()
	assert.NotNil(t, originalForm)

	// SetForm should replace the form.
	form.SetForm(originalForm)
	assert.Equal(t, originalForm, form.Form())
}

func TestBootstrapFormSaveOpenAIKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	bf := NewBootstrapForm(path)
	bf.Config().Provider.Default = "openai"
	bf.openaiKey = "sk-openai-test"

	err := bf.Save()
	require.NoError(t, err)

	loaded, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "openai", loaded.Provider.Default)
	require.Len(t, loaded.Provider.OpenAI, 1)
	assert.Equal(t, "openai", loaded.Provider.OpenAI[0].Name)
	assert.Equal(t, "https://api.openai.com/v1", loaded.Provider.OpenAI[0].BaseURL)
	assert.Equal(t, "sk-openai-test", loaded.Provider.OpenAI[0].APIKey)
	// Anthropic key should remain empty.
	assert.Empty(t, loaded.Provider.Anthropic.APIKey)
}

func TestBootstrapFormSaveOllamaNoKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	bf := NewBootstrapForm(path)
	bf.Config().Provider.Default = "ollama"

	err := bf.Save()
	require.NoError(t, err)

	loaded, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "ollama", loaded.Provider.Default)
	assert.Empty(t, loaded.Provider.Anthropic.APIKey)
	assert.Empty(t, loaded.Provider.OpenAI)
}

func TestBootstrapFormSaveOpenAICustomBaseURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	bf := NewBootstrapForm(path)
	bf.Config().Provider.Default = "openai"
	bf.openaiKey = "sk-custom-test"
	bf.openaiBaseURL = "https://openrouter.ai/api/v1"

	require.NoError(t, bf.Save())

	loaded, err := config.Load(path)
	require.NoError(t, err)
	require.Len(t, loaded.Provider.OpenAI, 1)
	assert.Equal(t, "https://openrouter.ai/api/v1", loaded.Provider.OpenAI[0].BaseURL)
}

func TestBootstrapFormSaveOpenAIDefaultBaseURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// When user leaves base URL empty, it should default to OpenAI.
	bf := NewBootstrapForm(path)
	bf.Config().Provider.Default = "openai"
	bf.openaiKey = "sk-default-test"

	require.NoError(t, bf.Save())

	loaded, err := config.Load(path)
	require.NoError(t, err)
	require.Len(t, loaded.Provider.OpenAI, 1)
	assert.Equal(t, "https://api.openai.com/v1", loaded.Provider.OpenAI[0].BaseURL)
}

func TestBootstrapOpenAIProviderRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// Simulate bootstrap wizard saving an OpenAI config.
	bf := NewBootstrapForm(path)
	bf.Config().Provider.Default = "openai"
	bf.Config().Provider.Model = "gpt-4o"
	bf.openaiKey = "sk-round-trip-test"
	require.NoError(t, bf.Save())

	// Load the saved config and verify NewProvider succeeds.
	loaded, err := config.Load(path)
	require.NoError(t, err)

	p, err := provider.NewProvider(loaded)
	require.NoError(t, err, "NewProvider should succeed with bootstrap-saved openai config")
	assert.NotNil(t, p)
}
