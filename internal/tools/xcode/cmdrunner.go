package xcode

import (
	"context"
	"os/exec"
)

// CommandRunner abstracts command execution for testability.
type CommandRunner interface {
	CombinedOutput(ctx context.Context, dir, name string, args ...string) ([]byte, error)
	Output(ctx context.Context, dir, name string, args ...string) ([]byte, error)
}

// ExecRunner implements CommandRunner using os/exec.
type ExecRunner struct{}

// CombinedOutput runs a command and returns combined stdout+stderr.
func (ExecRunner) CombinedOutput(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	return cmd.CombinedOutput()
}

// Output runs a command and returns stdout only.
func (ExecRunner) Output(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	return cmd.Output()
}

// MockRunner implements CommandRunner for tests.
type MockRunner struct {
	CombinedOutputFunc func(ctx context.Context, dir, name string, args ...string) ([]byte, error)
	OutputFunc         func(ctx context.Context, dir, name string, args ...string) ([]byte, error)
}

// CombinedOutput calls the configured function or returns empty output.
func (m *MockRunner) CombinedOutput(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	if m.CombinedOutputFunc != nil {
		return m.CombinedOutputFunc(ctx, dir, name, args...)
	}
	return nil, nil
}

// Output calls the configured function or returns empty output.
func (m *MockRunner) Output(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	if m.OutputFunc != nil {
		return m.OutputFunc(ctx, dir, name, args...)
	}
	return nil, nil
}
