// Package frontmatter parses YAML frontmatter from SKILL.md files used by
// built-in prompt skills. The format is: ---\nyaml\n---\nbody.
package frontmatter

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
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

	// Find the closing delimiter.
	idx := strings.Index(rest, delimiter)
	if idx < 0 {
		return "", "", "", fmt.Errorf("missing closing frontmatter delimiter")
	}

	yamlBlock := rest[:idx]
	body = strings.TrimSpace(rest[idx+len(delimiter):])

	var fm Fields
	if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
		return "", "", "", fmt.Errorf("parse frontmatter YAML: %w", err)
	}

	return fm.Name, fm.Description, body, nil
}
