package terminal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// lookupMmdc returns the path to mmdc or an error when it is not on PATH.
// Exposed as a package-level function so tests can call it directly.
func lookupMmdc() (string, error) {
	return exec.LookPath("mmdc")
}

// MmdcAvailable reports whether the Mermaid CLI (mmdc) is available on PATH.
func MmdcAvailable() bool {
	_, err := lookupMmdc()
	return err == nil
}

// RenderMermaid renders a Mermaid diagram to PNG bytes using the mmdc CLI.
// darkMode selects the "dark" theme; otherwise the "default" (light) theme is used.
// A transparent background and 800-pixel width are applied.
// Returns an error when mmdc is not found or rendering fails.
func RenderMermaid(ctx context.Context, mermaidSrc string, darkMode bool) ([]byte, error) {
	mmdcPath, err := lookupMmdc()
	if err != nil {
		return nil, fmt.Errorf("mmdc not found on PATH: %w", err)
	}

	dir, err := os.MkdirTemp("", "rubichan-mermaid-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	inputFile := filepath.Join(dir, "input.mmd")
	outputFile := filepath.Join(dir, "output.png")

	if err := os.WriteFile(inputFile, []byte(mermaidSrc), 0600); err != nil {
		return nil, fmt.Errorf("write mermaid source: %w", err)
	}

	theme := "default"
	if darkMode {
		theme = "dark"
	}

	cmd := exec.CommandContext(ctx, mmdcPath,
		"-i", inputFile,
		"-o", outputFile,
		"-t", theme,
		"-b", "transparent",
		"-w", "800",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("mmdc render failed: %w\noutput: %s", err, out)
	}

	png, err := os.ReadFile(outputFile)
	if err != nil {
		return nil, fmt.Errorf("read output PNG: %w", err)
	}
	return png, nil
}
