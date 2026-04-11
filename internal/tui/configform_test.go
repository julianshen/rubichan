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
	assert.Equal(t, 3, form.GroupCount())
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
