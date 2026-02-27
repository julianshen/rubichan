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

func TestConfigFormSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "ollama"

	form := NewConfigForm(cfg, path)
	err := form.Save()
	require.NoError(t, err)

	loaded, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "ollama", loaded.Provider.Default)
}
