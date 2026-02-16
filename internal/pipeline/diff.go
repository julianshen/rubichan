package pipeline

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ExtractDiff runs git diff in the given directory and returns the diff text.
// If diffRange is empty, defaults to "HEAD~1..HEAD".
func ExtractDiff(ctx context.Context, dir, diffRange string) (string, error) {
	if diffRange == "" {
		diffRange = "HEAD~1..HEAD"
	}

	parts := strings.SplitN(diffRange, "..", 2)
	args := []string{"diff"}
	if len(parts) == 2 {
		args = append(args, parts[0], parts[1])
	} else {
		args = append(args, diffRange)
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git diff failed: %s: %w", strings.TrimSpace(string(out)), err)
	}

	return string(out), nil
}
