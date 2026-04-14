package platform

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// ExecFunc is the signature CLI-backed Platform clients use to invoke
// external binaries (`gh`, `glab`). Tests inject a fake. stdin is optional
// (nil when the command takes no input).
type ExecFunc func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error)

// defaultExec runs name with args, piping stdin if non-nil, and returns
// the combined stdout. On failure, stderr is wrapped into the error so
// callers surface actionable CLI diagnostics.
func defaultExec(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s %v: %w (stderr: %s)",
			name, args, err, bytes.TrimSpace(stderr.Bytes()))
	}
	return stdout.Bytes(), nil
}
