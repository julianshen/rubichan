package wiki

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- test helpers ----------

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	assert.NoError(t, err, "file should exist: %s", path)
}

func assertFileContains(t *testing.T, path, substring string) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), substring)
}

// ---------- tests ----------

func TestDefaultRendererConfig(t *testing.T) {
	cfg := DefaultRendererConfig()
	assert.Equal(t, "raw-md", cfg.Format)
	assert.Equal(t, "docs/wiki", cfg.OutputDir)
}

func TestRenderRawMarkdown(t *testing.T) {
	outDir := t.TempDir()

	docs := []Document{
		{Path: "index.md", Title: "Home", Content: "# Welcome\nHello world."},
		{Path: "guide/setup.md", Title: "Setup", Content: "# Setup\nInstall steps."},
	}

	cfg := RendererConfig{Format: "raw-md", OutputDir: outDir}
	err := Render(docs, cfg)
	require.NoError(t, err)

	// Verify files exist and have correct content
	indexPath := filepath.Join(outDir, "index.md")
	assertFileExists(t, indexPath)
	data, err := os.ReadFile(indexPath)
	require.NoError(t, err)
	assert.Equal(t, "# Welcome\nHello world.", string(data))

	setupPath := filepath.Join(outDir, "guide", "setup.md")
	assertFileExists(t, setupPath)
	data, err = os.ReadFile(setupPath)
	require.NoError(t, err)
	assert.Equal(t, "# Setup\nInstall steps.", string(data))
}

func TestRenderHugo(t *testing.T) {
	outDir := t.TempDir()

	docs := []Document{
		{Path: "index.md", Title: "Home", Content: "# Welcome"},
		{Path: "modules/core.md", Title: "Core Module", Content: "# Core"},
	}

	cfg := RendererConfig{Format: "hugo", OutputDir: outDir}
	err := Render(docs, cfg)
	require.NoError(t, err)

	// Verify front matter on first doc
	indexPath := filepath.Join(outDir, "content", "index.md")
	assertFileExists(t, indexPath)
	assertFileContains(t, indexPath, "---\ntitle: \"Home\"\nweight: 1\n---\n\n")
	assertFileContains(t, indexPath, "# Welcome")

	// Verify front matter on second doc
	corePath := filepath.Join(outDir, "content", "modules", "core.md")
	assertFileExists(t, corePath)
	assertFileContains(t, corePath, "---\ntitle: \"Core Module\"\nweight: 2\n---\n\n")
	assertFileContains(t, corePath, "# Core")

	// Verify config.toml exists with Hugo configuration
	configPath := filepath.Join(outDir, "config.toml")
	assertFileExists(t, configPath)
	assertFileContains(t, configPath, "baseURL")
	assertFileContains(t, configPath, "title")
}

func TestRenderDocusaurus(t *testing.T) {
	outDir := t.TempDir()

	docs := []Document{
		{Path: "intro.md", Title: "Introduction", Content: "# Intro"},
		{Path: "api/reference.md", Title: "API Reference", Content: "# API"},
	}

	cfg := RendererConfig{Format: "docusaurus", OutputDir: outDir}
	err := Render(docs, cfg)
	require.NoError(t, err)

	// Verify front matter on first doc
	introPath := filepath.Join(outDir, "docs", "intro.md")
	assertFileExists(t, introPath)
	assertFileContains(t, introPath, "---\nsidebar_position: 1\nsidebar_label: \"Introduction\"\n---\n\n")
	assertFileContains(t, introPath, "# Intro")

	// Verify front matter on second doc
	apiPath := filepath.Join(outDir, "docs", "api", "reference.md")
	assertFileExists(t, apiPath)
	assertFileContains(t, apiPath, "---\nsidebar_position: 2\nsidebar_label: \"API Reference\"\n---\n\n")
	assertFileContains(t, apiPath, "# API")

	// Verify docusaurus.config.js
	configPath := filepath.Join(outDir, "docusaurus.config.js")
	assertFileExists(t, configPath)
	assertFileContains(t, configPath, "@docusaurus/theme-mermaid")
	assertFileContains(t, configPath, "mermaid: true")
}

func TestRenderUnsupportedFormat(t *testing.T) {
	outDir := t.TempDir()

	docs := []Document{
		{Path: "index.md", Title: "Home", Content: "# Welcome"},
	}

	cfg := RendererConfig{Format: "mkdocs", OutputDir: outDir}
	err := Render(docs, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported render format")
}

func TestRenderEmptyDocuments(t *testing.T) {
	outDir := t.TempDir()

	cfg := RendererConfig{Format: "raw-md", OutputDir: outDir}
	err := Render(nil, cfg)
	require.NoError(t, err)
}
