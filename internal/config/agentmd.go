package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// loadOptionalMarkdown reads an optional workspace markdown file.
// It trims only for the emptiness check and returns the original file content
// unchanged when the file exists and is non-empty.
func loadOptionalMarkdown(projectRoot, filename string) (string, error) {
	path := filepath.Join(projectRoot, filename)
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", filepathError("loadOptionalMarkdown", projectRoot, filename, "symlinks are not allowed")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return "", nil
	}
	return string(data), nil
}

func filepathError(fn, projectRoot, filename, message string) error {
	return &os.PathError{
		Op:   fn,
		Path: filepath.Join(projectRoot, filename),
		Err:  errString(message),
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// loadAgentMDRaw reads the raw AGENT.md content from the project root.
func loadAgentMDRaw(projectRoot string) (string, error) {
	return loadOptionalMarkdown(projectRoot, "AGENT.md")
}

// splitFrontmatter splits YAML frontmatter delimited by --- from body.
// Returns (body, frontmatter). If no frontmatter is present, frontmatter is empty.
func splitFrontmatter(content string) (body, frontmatter string) {
	if !strings.HasPrefix(content, "---\n") {
		return content, ""
	}

	// Find closing ---
	rest := content[4:] // skip opening "---\n"
	idx := strings.Index(rest, "\n---\n")
	if idx < 0 {
		// Check if it ends with \n---
		if strings.HasSuffix(rest, "\n---") {
			frontmatter = rest[:len(rest)-4]
			return "", frontmatter
		}
		return content, ""
	}

	frontmatter = rest[:idx]
	body = rest[idx+4:] // skip "\n---\n"
	return body, frontmatter
}

// agentMDFrontmatter is the YAML structure for AGENT.md frontmatter.
type agentMDFrontmatter struct {
	Hooks []HookRuleConfig `yaml:"hooks"`
}

// LoadAgentMD reads an AGENT.md file from the given project root directory.
// Returns the file content with any frontmatter stripped, or an empty string
// if the file does not exist.
func LoadAgentMD(projectRoot string) (string, error) {
	raw, err := loadAgentMDRaw(projectRoot)
	if err != nil || raw == "" {
		return raw, err
	}
	body, _ := splitFrontmatter(raw)
	return body, nil
}

// LoadAgentMDWithHooks reads an AGENT.md file and parses YAML frontmatter
// for hook configurations. Returns the body (without frontmatter), parsed
// hooks, and any error.
func LoadAgentMDWithHooks(projectRoot string) (string, []HookRuleConfig, error) {
	raw, err := loadAgentMDRaw(projectRoot)
	if err != nil {
		return "", nil, err
	}
	if raw == "" {
		return "", nil, nil
	}

	body, fm := splitFrontmatter(raw)
	if fm == "" {
		return body, nil, nil
	}

	var parsed agentMDFrontmatter
	if err := yaml.Unmarshal([]byte(fm), &parsed); err != nil {
		return "", nil, fmt.Errorf("parsing AGENT.md frontmatter: %w", err)
	}

	return body, parsed.Hooks, nil
}

// LoadIdentityMD reads an IDENTITY.md file from the given project root.
func LoadIdentityMD(projectRoot string) (string, error) {
	return loadOptionalMarkdown(projectRoot, "IDENTITY.md")
}

// LoadSoulMD reads a SOUL.md file from the given project root.
func LoadSoulMD(projectRoot string) (string, error) {
	return loadOptionalMarkdown(projectRoot, "SOUL.md")
}
