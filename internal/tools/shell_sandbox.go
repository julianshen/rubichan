package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/julianshen/rubichan/internal/config"
)

// ShellSandbox wraps shell executions in an OS-specific sandbox backend.
type ShellSandbox interface {
	Name() string
	Wrap(cmd *exec.Cmd) error
}

// ShellSandboxPolicy describes the filesystem and network policy applied to a
// shell command when a platform backend is available.
type ShellSandboxPolicy struct {
	ProxyPort     int
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

	return ShellSandboxPolicy{
		AllowedPaths:  normalizeSandboxPaths(allowed),
		WritablePaths: normalizeSandboxPaths(writable),
		AllowSubprocs: true,
	}
}

// BuildSandboxPolicy creates a ShellSandboxPolicy from defaults plus config overrides.
// Config paths are appended to defaults, not replacing them.
func BuildSandboxPolicy(workDir string, cfg config.SandboxConfig) ShellSandboxPolicy {
	policy := DefaultShellSandboxPolicy(workDir)
	policy.ProxyPort = cfg.Network.ProxyPort
	policy.WritablePaths = append(policy.WritablePaths, normalizeSandboxPaths(cfg.Filesystem.AllowWrite)...)
	policy.DeniedPaths = append(policy.DeniedPaths, normalizeSandboxPaths(cfg.Filesystem.DenyRead)...)
	return policy
}

// NewDefaultShellSandbox selects the best available platform sandbox backend.
func NewDefaultShellSandbox(workDir string) ShellSandbox {
	return selectShellSandbox(runtime.GOOS, workDir, exec.LookPath, sandboxBackendAvailable)
}

type lookPathFunc func(file string) (string, error)
type sandboxProbeFunc func(goos, binary, workDir string) bool

func selectShellSandbox(goos, workDir string, lookPath lookPathFunc, probe sandboxProbeFunc) ShellSandbox {
	policy := DefaultShellSandboxPolicy(workDir)

	switch goos {
	case "darwin":
		if binary, err := lookPath("sandbox-exec"); err == nil {
			if probe != nil && !probe(goos, binary, workDir) {
				return nil
			}
			return &seatbeltSandbox{binary: binary, policy: policy}
		}
	case "linux":
		if binary, err := lookPath("bwrap"); err == nil {
			if probe != nil && !probe(goos, binary, workDir) {
				return nil
			}
			return &bubblewrapSandbox{binary: binary, policy: policy}
		}
	}

	return nil
}

func sandboxBackendAvailable(goos, binary, workDir string) bool {
	switch goos {
	case "darwin":
		// Defensive branch: current callers pass sandbox-exec, but keep this
		// guard in case a future caller probes a different wrapper binary.
		if filepath.Base(binary) != "sandbox-exec" {
			return true
		}

		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		sb := &seatbeltSandbox{
			binary: binary,
			policy: DefaultShellSandboxPolicy(workDir),
		}
		return runSandboxProbe(ctx, workDir, sb.Wrap)
	case "linux":
		// Defensive branch: current callers pass bwrap, but keep this guard in
		// case a future caller probes a different wrapper binary.
		if filepath.Base(binary) != "bwrap" {
			return true
		}

		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		sb := &bubblewrapSandbox{
			binary: binary,
			policy: DefaultShellSandboxPolicy(workDir),
		}
		return runSandboxProbe(ctx, workDir, sb.Wrap)
	default:
		return true
	}
}

func runSandboxProbe(ctx context.Context, workDir string, wrap func(*exec.Cmd) error) bool {
	cmd := exec.CommandContext(ctx, "sh", "-c", "pwd")
	cmd.Dir = workDir
	if err := wrap(cmd); err != nil {
		return false
	}
	return cmd.Run() == nil
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
	cmd.Env = sandboxCommandEnv(cmd, s.policy.ProxyPort)
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
	if !s.policy.AllowSubprocs {
		return fmt.Errorf("subprocesses disabled by policy: bubblewrap backend does not support this on Linux")
	}

	args := []string{
		s.binary,
		"--die-with-parent",
		"--new-session",
		"--unshare-all",
		"--proc", "/proc",
		"--dev", "/dev",
	}
	if s.policy.ProxyPort > 0 {
		args = append(args, "--share-net")
	}

	for _, path := range existingSandboxPaths(s.policy.AllowedPaths) {
		args = append(args, "--ro-bind", path, path)
	}
	for _, path := range existingSandboxPaths(s.policy.WritablePaths) {
		args = append(args, "--bind", path, path)
	}
	for _, path := range existingSandboxPaths(s.policy.DeniedPaths) {
		if isDir(path) {
			args = append(args, "--tmpfs", path)
			continue
		}
		args = append(args, "--ro-bind", "/dev/null", path)
	}
	if cmd.Dir != "" {
		args = append(args, "--chdir", cmd.Dir)
	}

	cmd.Env = sandboxCommandEnv(cmd, s.policy.ProxyPort)
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
	allowed := normalizeSandboxPaths(policy.AllowedPaths)
	writable := normalizeSandboxPaths(policy.WritablePaths)
	denied := normalizeSandboxPaths(policy.DeniedPaths)
	sort.SliceStable(denied, func(i, j int) bool {
		return len(denied[i]) > len(denied[j])
	})

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
	if policy.ProxyPort > 0 {
		lines = append(lines, fmt.Sprintf("(allow network-outbound (remote ip \"127.0.0.1\") (remote tcp \"*:%d\"))", policy.ProxyPort))
	}
	for _, path := range allowed {
		lines = append(lines, fmt.Sprintf("(allow file-read* (subpath %q))", path))
	}
	for _, path := range writable {
		lines = append(lines, fmt.Sprintf("(allow file-read* (subpath %q))", path))
		lines = append(lines, fmt.Sprintf("(allow file-write* (subpath %q))", path))
	}
	for _, path := range denied {
		lines = append(lines, fmt.Sprintf("(deny file-read* file-write* (subpath %q))", path))
	}
	return strings.Join(lines, "\n")
}

func sandboxCommandEnv(cmd *exec.Cmd, proxyPort int) []string {
	env := cmd.Environ()
	sandboxHome := filepath.Join(cmd.Dir, ".rubichan-sandbox")
	if cmd.Dir == "" {
		sandboxHome = filepath.Join(os.TempDir(), "rubichan-sandbox")
	}
	values := map[string]string{
		"HOME":            sandboxHome,
		"XDG_CACHE_HOME":  filepath.Join(sandboxHome, ".cache"),
		"XDG_CONFIG_HOME": filepath.Join(sandboxHome, ".config"),
	}
	if proxyPort > 0 {
		proxyURL := fmt.Sprintf("http://127.0.0.1:%d", proxyPort)
		values["HTTP_PROXY"] = proxyURL
		values["HTTPS_PROXY"] = proxyURL
		values["http_proxy"] = proxyURL
		values["https_proxy"] = proxyURL
		values["NO_PROXY"] = "localhost,127.0.0.1"
		values["no_proxy"] = "localhost,127.0.0.1"
	}
	return setEnvValues(env, values)
}

func setEnvValues(env []string, values map[string]string) []string {
	out := make([]string, 0, len(env)+len(values))
	remaining := make(map[string]string, len(values))
	for k, v := range values {
		remaining[k] = v
	}

	for _, entry := range env {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			out = append(out, entry)
			continue
		}
		if value, exists := remaining[name]; exists {
			out = append(out, name+"="+value)
			delete(remaining, name)
			continue
		}
		out = append(out, entry)
	}
	for name, value := range remaining {
		out = append(out, name+"="+value)
	}
	return out
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

func isDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
