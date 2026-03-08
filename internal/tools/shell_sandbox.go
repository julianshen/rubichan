package tools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// ShellSandbox wraps shell executions in an OS-specific sandbox backend.
type ShellSandbox interface {
	Name() string
	Wrap(cmd *exec.Cmd) error
}

// ShellSandboxPolicy describes the filesystem and network policy applied to a
// shell command when a platform backend is available.
type ShellSandboxPolicy struct {
	AllowNetwork  bool
	AllowedPaths  []string
	WritablePaths []string
	DeniedPaths   []string
	AllowSubprocs bool
}

// DefaultShellSandboxPolicy restricts commands to the project workspace plus
// standard runtime directories required to launch shell processes.
func DefaultShellSandboxPolicy(workDir string) ShellSandboxPolicy {
	allowed := []string{
		workDir,
		"/bin",
		"/dev",
		"/etc",
		"/System",
		"/Library",
		"/lib",
		"/lib64",
		"/usr",
		"/usr/bin",
		"/usr/lib",
		"/usr/libexec",
		"/usr/local",
	}

	writable := []string{workDir, os.TempDir()}
	if tmpDir := os.Getenv("TMPDIR"); tmpDir != "" {
		writable = append(writable, tmpDir)
	}
	if homeDir, err := os.UserHomeDir(); err == nil {
		writable = append(writable,
			filepath.Join(homeDir, ".cache"),
			filepath.Join(homeDir, ".config"),
			filepath.Join(homeDir, "Library", "Caches"),
			filepath.Join(homeDir, "Library", "Application Support"),
		)
	}
	if xdgCache := os.Getenv("XDG_CACHE_HOME"); xdgCache != "" {
		writable = append(writable, xdgCache)
	}
	if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		writable = append(writable, xdgConfig)
	}

	return ShellSandboxPolicy{
		AllowedPaths:  normalizeSandboxPaths(allowed),
		WritablePaths: normalizeSandboxPaths(writable),
		AllowSubprocs: true,
	}
}

// NewDefaultShellSandbox selects the best available platform sandbox backend.
func NewDefaultShellSandbox(workDir string) ShellSandbox {
	return selectShellSandbox(runtime.GOOS, workDir, exec.LookPath)
}

type lookPathFunc func(file string) (string, error)

func selectShellSandbox(goos, workDir string, lookPath lookPathFunc) ShellSandbox {
	policy := DefaultShellSandboxPolicy(workDir)

	switch goos {
	case "darwin":
		if binary, err := lookPath("sandbox-exec"); err == nil {
			return &seatbeltSandbox{binary: binary, policy: policy}
		}
	case "linux":
		if binary, err := lookPath("bwrap"); err == nil {
			return &bubblewrapSandbox{binary: binary, policy: policy}
		}
	}

	return nil
}

type seatbeltSandbox struct {
	binary string
	policy ShellSandboxPolicy
}

func (s *seatbeltSandbox) Name() string {
	return "seatbelt"
}

func (s *seatbeltSandbox) Wrap(cmd *exec.Cmd) error {
	originalPath, originalArgs, err := resolveWrappedCommand(cmd)
	if err != nil {
		return err
	}

	profile := buildSeatbeltProfile(s.policy)
	cmd.Path = s.binary
	cmd.Args = append([]string{s.binary, "-p", profile, originalPath}, originalArgs[1:]...)
	return nil
}

type bubblewrapSandbox struct {
	binary string
	policy ShellSandboxPolicy
}

func (s *bubblewrapSandbox) Name() string {
	return "bubblewrap"
}

func (s *bubblewrapSandbox) Wrap(cmd *exec.Cmd) error {
	originalPath, originalArgs, err := resolveWrappedCommand(cmd)
	if err != nil {
		return err
	}

	args := []string{
		s.binary,
		"--die-with-parent",
		"--new-session",
		"--unshare-all",
		"--proc", "/proc",
		"--dev", "/dev",
	}
	if s.policy.AllowNetwork {
		args = append(args, "--share-net")
	}

	for _, path := range existingSandboxPaths(s.policy.AllowedPaths) {
		args = append(args, "--ro-bind", path, path)
	}
	for _, path := range existingSandboxPaths(s.policy.WritablePaths) {
		args = append(args, "--bind", path, path)
	}
	for _, path := range existingSandboxPaths(s.policy.DeniedPaths) {
		args = append(args, "--tmpfs", path)
	}
	if cmd.Dir != "" {
		args = append(args, "--chdir", cmd.Dir)
	}

	args = append(args, "--", originalPath)
	args = append(args, originalArgs[1:]...)

	cmd.Path = s.binary
	cmd.Args = args
	return nil
}

func resolveWrappedCommand(cmd *exec.Cmd) (string, []string, error) {
	if cmd.Path == "" && len(cmd.Args) == 0 {
		return "", nil, fmt.Errorf("missing command path")
	}

	originalPath := cmd.Path
	if originalPath == "" {
		originalPath = cmd.Args[0]
	}
	if !filepath.IsAbs(originalPath) {
		resolved, err := exec.LookPath(originalPath)
		if err != nil {
			return "", nil, fmt.Errorf("resolve command path %q: %w", originalPath, err)
		}
		originalPath = resolved
	}

	originalArgs := append([]string{originalPath}, cmd.Args[1:]...)
	return originalPath, originalArgs, nil
}

func buildSeatbeltProfile(policy ShellSandboxPolicy) string {
	lines := []string{
		"(version 1)",
		"(deny default)",
		"(allow signal (target self))",
		"(allow sysctl-read)",
		"(allow process-exec)",
	}
	if policy.AllowSubprocs {
		lines = append(lines, "(allow process-fork)")
	}
	if !policy.AllowNetwork {
		lines = append(lines, "(deny network*)")
	}
	for _, path := range normalizeSandboxPaths(policy.AllowedPaths) {
		lines = append(lines, fmt.Sprintf("(allow file-read* (subpath %q))", path))
	}
	for _, path := range normalizeSandboxPaths(policy.WritablePaths) {
		lines = append(lines, fmt.Sprintf("(allow file-read* (subpath %q))", path))
		lines = append(lines, fmt.Sprintf("(allow file-write* (subpath %q))", path))
	}
	for _, path := range normalizeSandboxPaths(policy.DeniedPaths) {
		lines = append(lines, fmt.Sprintf("(deny file-read* file-write* (subpath %q))", path))
	}
	return strings.Join(lines, "\n")
}

func normalizeSandboxPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		cleaned := filepath.Clean(path)
		if slices.Contains(out, cleaned) {
			continue
		}
		out = append(out, cleaned)
	}
	return out
}

func existingSandboxPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range normalizeSandboxPaths(paths) {
		if _, err := os.Stat(path); err == nil {
			out = append(out, path)
		}
	}
	return out
}
