package xcode

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// PlatformChecker abstracts platform detection for testability.
type PlatformChecker interface {
	IsDarwin() bool
	XcodePath() (string, error)
}

// RealPlatformChecker uses runtime.GOOS and xcode-select.
type RealPlatformChecker struct{}

// NewRealPlatformChecker creates a new RealPlatformChecker.
func NewRealPlatformChecker() *RealPlatformChecker {
	return &RealPlatformChecker{}
}

// IsDarwin returns true if the current OS is macOS.
func (r *RealPlatformChecker) IsDarwin() bool {
	return runtime.GOOS == "darwin"
}

// XcodePath returns the active Xcode developer directory via xcode-select.
func (r *RealPlatformChecker) XcodePath() (string, error) {
	if !r.IsDarwin() {
		return "", fmt.Errorf("Xcode is only available on macOS")
	}
	out, err := exec.Command("xcode-select", "-p").Output()
	if err != nil {
		return "", fmt.Errorf("xcode-select failed: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// MockPlatformChecker is used in tests.
type MockPlatformChecker struct {
	Darwin       bool
	XcodeBinPath string
}

// IsDarwin returns the configured Darwin value.
func (m *MockPlatformChecker) IsDarwin() bool {
	return m.Darwin
}

// XcodePath returns the configured XcodeBinPath or an error if not Darwin.
func (m *MockPlatformChecker) XcodePath() (string, error) {
	if !m.Darwin {
		return "", fmt.Errorf("Xcode is only available on macOS")
	}
	return m.XcodeBinPath, nil
}
