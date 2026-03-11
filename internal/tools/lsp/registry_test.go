package lsp

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistryDefaults(t *testing.T) {
	r := NewRegistry()

	// Go should be configured by default.
	cfg, err := r.ConfigFor("go")
	require.NoError(t, err)
	assert.Equal(t, "gopls", cfg.Command)
	assert.Equal(t, []string{"serve"}, cfg.Args)

	// TypeScript should be configured.
	cfg, err = r.ConfigFor("typescript")
	require.NoError(t, err)
	assert.Equal(t, "typescript-language-server", cfg.Command)
}

func TestRegistryLanguageForExt(t *testing.T) {
	r := NewRegistry()

	tests := []struct {
		ext  string
		want string
	}{
		{".go", "go"},
		{".ts", "typescript"},
		{".tsx", "typescript"},
		{".py", "python"},
		{".rs", "rust"},
		{".swift", "swift"},
		{".hs", "haskell"},
	}

	for _, tt := range tests {
		lang, ok := r.LanguageForExt(tt.ext)
		assert.True(t, ok, "expected mapping for %s", tt.ext)
		assert.Equal(t, tt.want, lang)
	}

	// Unknown extension.
	_, ok := r.LanguageForExt(".xyz")
	assert.False(t, ok)
}

func TestRegistryLanguageForExtWithoutDot(t *testing.T) {
	r := NewRegistry()

	lang, ok := r.LanguageForExt("go")
	assert.True(t, ok)
	assert.Equal(t, "go", lang)
}

func TestRegistryLanguageForFile(t *testing.T) {
	r := NewRegistry()

	lang, ok := r.LanguageForFile("/home/user/project/main.go")
	assert.True(t, ok)
	assert.Equal(t, "go", lang)

	lang, ok = r.LanguageForFile("src/index.tsx")
	assert.True(t, ok)
	assert.Equal(t, "typescript", lang)

	_, ok = r.LanguageForFile("Makefile")
	assert.False(t, ok)
}

func TestRegistryConfigForUnknown(t *testing.T) {
	r := NewRegistry()

	_, err := r.ConfigFor("brainfuck")
	assert.ErrorIs(t, err, ErrNoConfig)
}

func TestRegistryOverride(t *testing.T) {
	r := NewRegistry()

	// Override the Go config.
	r.Register(ServerConfig{
		Language:   "go",
		Command:    "custom-gopls",
		Args:       []string{"--custom"},
		Extensions: []string{".go"},
	})

	cfg, err := r.ConfigFor("go")
	require.NoError(t, err)
	assert.Equal(t, "custom-gopls", cfg.Command)
	assert.Equal(t, []string{"--custom"}, cfg.Args)
}

func TestRegistryAddCustomLanguage(t *testing.T) {
	r := NewRegistry()

	r.Register(ServerConfig{
		Language:   "gleam",
		Command:    "gleam",
		Args:       []string{"lsp"},
		Extensions: []string{".gleam"},
	})

	cfg, err := r.ConfigFor("gleam")
	require.NoError(t, err)
	assert.Equal(t, "gleam", cfg.Command)

	lang, ok := r.LanguageForExt(".gleam")
	assert.True(t, ok)
	assert.Equal(t, "gleam", lang)
}

func TestRegistryIsInstalled(t *testing.T) {
	r := NewRegistry()

	// Mock lookPath to simulate installed/not installed.
	r.lookPath = func(name string) (string, error) {
		if name == "gopls" {
			return "/usr/bin/gopls", nil
		}
		return "", fmt.Errorf("not found: %s", name)
	}

	assert.True(t, r.IsInstalled("go"))
	assert.False(t, r.IsInstalled("rust"))
	assert.False(t, r.IsInstalled("nonexistent"))
}

func TestRegistryAvailable(t *testing.T) {
	r := NewRegistry()

	installed := map[string]bool{
		"gopls":              true,
		"rust-analyzer":      true,
		"pyright-langserver": true,
	}

	r.lookPath = func(name string) (string, error) {
		if installed[name] {
			return "/usr/bin/" + name, nil
		}
		return "", fmt.Errorf("not found: %s", name)
	}

	available := r.Available()
	assert.Contains(t, available, "go")
	assert.Contains(t, available, "rust")
	assert.Contains(t, available, "python")
	assert.NotContains(t, available, "typescript")
}

func TestRegistryLanguages(t *testing.T) {
	r := NewRegistry()

	langs := r.Languages()
	assert.Contains(t, langs, "go")
	assert.Contains(t, langs, "typescript")
	assert.Contains(t, langs, "python")
	assert.GreaterOrEqual(t, len(langs), 18)
}
