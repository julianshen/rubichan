// Package frontenddesign embeds the anthropics/claude-code frontend-design
// prompt skill as a built-in skill. The SKILL.md has YAML frontmatter with
// name and description, and the body becomes the inline system prompt.
package frontenddesign

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/julianshen/rubichan/internal/skills"
)

//go:embed content
var content embed.FS

// skillFrontmatter holds the YAML fields parsed from SKILL.md frontmatter.
type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// parseFrontmatter splits a SKILL.md file into frontmatter fields and body.
func parseFrontmatter(raw string) (name, description, body string, err error) {
	const delimiter = "---"
	if !strings.HasPrefix(raw, delimiter) {
		return "", "", "", fmt.Errorf("missing opening frontmatter delimiter")
	}

	rest := raw[len(delimiter)+1:]
	idx := strings.Index(rest, delimiter)
	if idx < 0 {
		return "", "", "", fmt.Errorf("missing closing frontmatter delimiter")
	}

	yamlBlock := rest[:idx]
	body = strings.TrimSpace(rest[idx+len(delimiter):])

	var fm skillFrontmatter
	if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
		return "", "", "", fmt.Errorf("parse frontmatter YAML: %w", err)
	}

	return fm.Name, fm.Description, body, nil
}

// Register walks the embedded content directory and registers each skill as a
// built-in prompt skill on the loader. Skills auto-activate in interactive mode.
func Register(loader *skills.Loader) {
	fs.WalkDir(content, "content", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "SKILL.md" {
			return err
		}

		data, readErr := content.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		name, description, body, parseErr := parseFrontmatter(string(data))
		if parseErr != nil {
			return parseErr
		}

		m := &skills.SkillManifest{
			Name:        name,
			Version:     "1.0.0",
			Description: description,
			Types:       []skills.SkillType{skills.SkillTypePrompt},
			Prompt: skills.PromptConfig{
				SystemPromptFile: body,
			},
			Triggers: skills.TriggerConfig{
				Modes: []string{"interactive"},
			},
		}
		loader.RegisterBuiltin(m)
		return nil
	})
}
