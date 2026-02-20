package wiki

import (
	"fmt"
	"os"
	"path/filepath"
)

// RendererConfig controls how the site renderer writes output files.
type RendererConfig struct {
	Format    string // "raw-md", "hugo", or "docusaurus"
	OutputDir string // root output directory
}

// DefaultRendererConfig returns a RendererConfig with sensible defaults.
func DefaultRendererConfig() RendererConfig {
	return RendererConfig{
		Format:    "raw-md",
		OutputDir: "docs/wiki",
	}
}

// Render writes the given documents to disk in the configured format.
func Render(documents []Document, cfg RendererConfig) error {
	switch cfg.Format {
	case "raw-md":
		return renderRawMarkdown(documents, cfg)
	case "hugo":
		return renderHugo(documents, cfg)
	case "docusaurus":
		return renderDocusaurus(documents, cfg)
	default:
		return fmt.Errorf("unsupported render format: %s", cfg.Format)
	}
}

// renderRawMarkdown writes each document as-is under OutputDir.
func renderRawMarkdown(documents []Document, cfg RendererConfig) error {
	for _, doc := range documents {
		path := filepath.Join(cfg.OutputDir, doc.Path)
		if err := writeDoc(path, doc.Content); err != nil {
			return err
		}
	}
	return nil
}

// renderHugo writes documents with YAML front matter under OutputDir/content/
// and generates a config.toml at OutputDir/config.toml.
func renderHugo(documents []Document, cfg RendererConfig) error {
	for i, doc := range documents {
		frontMatter := fmt.Sprintf("---\ntitle: %q\nweight: %d\n---\n\n", doc.Title, i+1)
		path := filepath.Join(cfg.OutputDir, "content", doc.Path)
		if err := writeDoc(path, frontMatter+doc.Content); err != nil {
			return err
		}
	}

	configContent := `baseURL = "/"
languageCode = "en-us"
title = "Project Wiki"
theme = "hugo-book"
`
	configPath := filepath.Join(cfg.OutputDir, "config.toml")
	if err := writeDoc(configPath, configContent); err != nil {
		return err
	}

	return nil
}

// renderDocusaurus writes documents with YAML front matter under OutputDir/docs/
// and generates a docusaurus.config.js at OutputDir/docusaurus.config.js.
func renderDocusaurus(documents []Document, cfg RendererConfig) error {
	for i, doc := range documents {
		frontMatter := fmt.Sprintf("---\nsidebar_position: %d\nsidebar_label: %q\n---\n\n", i+1, doc.Title)
		path := filepath.Join(cfg.OutputDir, "docs", doc.Path)
		if err := writeDoc(path, frontMatter+doc.Content); err != nil {
			return err
		}
	}

	configContent := `// @ts-check

/** @type {import('@docusaurus/types').Config} */
const config = {
  title: 'Project Wiki',
  url: 'https://your-project-url.example.com',
  baseUrl: '/',
  themes: ['@docusaurus/theme-mermaid'],
  markdown: {
    mermaid: true,
  },
  presets: [
    [
      'classic',
      /** @type {import('@docusaurus/preset-classic').Options} */
      ({
        docs: {
          routeBasePath: '/',
        },
      }),
    ],
  ],
};

module.exports = config;
`
	configPath := filepath.Join(cfg.OutputDir, "docusaurus.config.js")
	if err := writeDoc(configPath, configContent); err != nil {
		return err
	}

	return nil
}

// writeDoc creates parent directories and writes content to the given path.
func writeDoc(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
