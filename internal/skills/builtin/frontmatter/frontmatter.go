// Package frontmatter parses YAML frontmatter from SKILL.md files used by
// built-in prompt skills. The format is: ---\nyaml\n---\nbody.
package frontmatter

import (
	"fmt"
	"io/fs"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/julianshen/rubichan/internal/skills"
)

// Fields holds the YAML fields parsed from SKILL.md frontmatter.
type Fields struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// Parse splits a SKILL.md file into frontmatter fields and body content.
// The file must start with "---\n", followed by YAML, then "---\n", then body.
func Parse(raw string) (name, description, body string, err error) {
	const delimiter = "---"
	if !strings.HasPrefix(raw, delimiter) {
		return "", "", "", fmt.Errorf("missing opening frontmatter delimiter")
	}

	// Find the first newline after the opening delimiter.
	firstNewline := strings.Index(raw, "\n")
	if firstNewline < 0 {
		return "", "", "", fmt.Errorf("missing content after opening delimiter")
	}
	rest := raw[firstNewline+1:]

	// Find the closing delimiter at the start of a line.
	// Look for "\n---" so we don't match "---" as a substring of other content.
	closingMarker := "\n" + delimiter
	idx := strings.Index(rest, closingMarker)
	if idx < 0 {
		return "", "", "", fmt.Errorf("missing closing frontmatter delimiter")
	}

	yamlBlock := rest[:idx]
	body = strings.TrimSpace(rest[idx+len(closingMarker):])

	var fm Fields
	if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
		return "", "", "", fmt.Errorf("parse frontmatter YAML: %w", err)
	}

	return fm.Name, fm.Description, body, nil
}

// RegisterAll walks an embedded FS for SKILL.md files and registers each as a
// built-in prompt skill that auto-activates in interactive mode. It returns an
// error if any embedded content is malformed.
func RegisterAll(fsys fs.FS, loader *skills.Loader) error {
	return fs.WalkDir(fsys, "content", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || d.Name() != "SKILL.md" {
			return walkErr
		}

		data, readErr := fs.ReadFile(fsys, path)
		if readErr != nil {
			return readErr
		}

		name, description, body, parseErr := Parse(string(data))
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
