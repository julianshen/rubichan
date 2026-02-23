package config

import (
	"os"
	"path/filepath"
	"strings"
)

// LoadAgentMD reads an AGENT.md file from the given project root directory.
// Returns the file content, or an empty string if the file does not exist.
func LoadAgentMD(projectRoot string) (string, error) {
	data, err := os.ReadFile(filepath.Join(projectRoot, "AGENT.md"))
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
