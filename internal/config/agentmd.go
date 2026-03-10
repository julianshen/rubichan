package config

import (
	"os"
	"path/filepath"
	"strings"
)

// loadOptionalMarkdown reads an optional workspace markdown file.
// It trims only for the emptiness check and returns the original file content
// unchanged when the file exists and is non-empty.
func loadOptionalMarkdown(projectRoot, filename string) (string, error) {
	data, err := os.ReadFile(filepath.Join(projectRoot, filename))
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

// LoadAgentMD reads an AGENT.md file from the given project root directory.
// Returns the file content, or an empty string if the file does not exist.
func LoadAgentMD(projectRoot string) (string, error) {
	return loadOptionalMarkdown(projectRoot, "AGENT.md")
}

// LoadIdentityMD reads an IDENTITY.md file from the given project root.
func LoadIdentityMD(projectRoot string) (string, error) {
	return loadOptionalMarkdown(projectRoot, "IDENTITY.md")
}

// LoadSoulMD reads a SOUL.md file from the given project root.
func LoadSoulMD(projectRoot string) (string, error) {
	return loadOptionalMarkdown(projectRoot, "SOUL.md")
}
