// internal/runner/input.go
package runner

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// ResolveInput determines the user prompt from the available sources.
// Priority: promptFlag > filePath > stdinReader.
// stdinReader may be nil if stdin is a TTY (no pipe).
func ResolveInput(promptFlag, filePath string, stdinReader io.Reader) (string, error) {
	if text := strings.TrimSpace(promptFlag); text != "" {
		return text, nil
	}

	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("reading prompt file: %w", err)
		}
		text := strings.TrimSpace(string(data))
		if text == "" {
			return "", fmt.Errorf("prompt file is empty: %s", filePath)
		}
		return text, nil
	}

	if stdinReader != nil {
		data, err := io.ReadAll(stdinReader)
		if err != nil {
			return "", fmt.Errorf("reading stdin: %w", err)
		}
		if text := strings.TrimSpace(string(data)); text != "" {
			return text, nil
		}
	}

	return "", fmt.Errorf("no input provided: use --prompt, --file, or pipe to stdin")
}
