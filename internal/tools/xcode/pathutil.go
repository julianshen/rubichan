package xcode

import (
	"fmt"
	"path/filepath"
	"strings"
)

// validatePath checks that path, resolved relative to rootDir, does not escape
// rootDir. It returns the cleaned absolute path or an error.
func validatePath(rootDir, path string) (string, error) {
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("absolute paths are not allowed: %s", path)
	}
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return "", fmt.Errorf("resolving root dir: %w", err)
	}
	joined := filepath.Join(absRoot, path)
	cleaned := filepath.Clean(joined)
	if !strings.HasPrefix(cleaned, absRoot+string(filepath.Separator)) && cleaned != absRoot {
		return "", fmt.Errorf("path escapes project directory: %s", path)
	}
	return cleaned, nil
}
