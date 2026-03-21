package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectShellSandboxDarwinUsesSeatbeltWhenAvailable(t *testing.T) {
	sb := selectShellSandbox("darwin", t.TempDir(), func(file string) (string, error) {
		if file == "sandbox-exec" {
			return "/usr/bin/sandbox-exec", nil
		}
		return "", errors.New("unexpected lookup")
	}, func(_, _, _ string) bool { return true })

	require.NotNil(t, sb)
	assert.Equal(t, "seatbelt", sb.Name())
}

func TestSelectShellSandboxLinuxUsesBubblewrapWhenAvailable(t *testing.T) {
	sb := selectShellSandbox("linux", t.TempDir(), func(file string) (string, error) {
		if file == "bwrap" {
			return "/usr/bin/bwrap", nil
		}
		return "", errors.New("unexpected lookup")
	}, func(_, _, _ string) bool { return true })

	require.NotNil(t, sb)
	assert.Equal(t, "bubblewrap", sb.Name())
}

func TestSelectShellSandboxFallsBackWhenBackendMissing(t *testing.T) {
	sb := selectShellSandbox("linux", t.TempDir(), func(string) (string, error) {
		return "", errors.New("missing")
	}, func(_, _, _ string) bool { return true })

	assert.Nil(t, sb)
}

func TestSelectShellSandboxReturnsNilWhenProbeRejects(t *testing.T) {
	sb := selectShellSandbox("linux", t.TempDir(), func(file string) (string, error) {
		if file == "bwrap" {
			return "/usr/bin/bwrap", nil
		}
		return "", errors.New("unexpected lookup")
	}, func(_, _, _ string) bool { return false })

	assert.Nil(t, sb)
}

func TestSeatbeltSandboxWrapsCommand(t *testing.T) {
	expectedShell, err := exec.LookPath("sh")
	require.NoError(t, err)

	sb := &seatbeltSandbox{
		binary: "/usr/bin/sandbox-exec",
		policy: ShellSandboxPolicy{
			AllowedPaths:  []string{"/bin"},
			WritablePaths: []string{"/tmp/work"},
			AllowSubprocs: true,
		},
	}
	cmd := exec.Command("sh", "-c", "echo hello")

	err = sb.Wrap(cmd)
	require.NoError(t, err)
	assert.Equal(t, "/usr/bin/sandbox-exec", cmd.Path)
	assert.Equal(t, "/usr/bin/sandbox-exec", cmd.Args[0])
	assert.Contains(t, cmd.Args, expectedShell)
	assert.Contains(t, cmd.Args[2], "(deny default)")
	assert.Contains(t, cmd.Args[2], `(allow file-write* (subpath "/tmp/work"))`)
}

func TestBubblewrapSandboxWrapsCommand(t *testing.T) {
	workDir := t.TempDir()
	expectedShell, err := exec.LookPath("sh")
	require.NoError(t, err)
	sb := &bubblewrapSandbox{
		binary: "/usr/bin/bwrap",
		policy: ShellSandboxPolicy{
			AllowedPaths:  []string{"/bin"},
			WritablePaths: []string{workDir},
			AllowSubprocs: true,
		},
	}
	cmd := exec.Command("sh", "-c", "echo hello")
	cmd.Dir = workDir

	err = sb.Wrap(cmd)
	require.NoError(t, err)
	assert.Equal(t, "/usr/bin/bwrap", cmd.Path)
	assert.Equal(t, "/usr/bin/bwrap", cmd.Args[0])
	assert.Contains(t, cmd.Args, "--unshare-all")
	assert.Contains(t, cmd.Args, "--chdir")
	assert.Contains(t, cmd.Args, workDir)
	assert.Contains(t, cmd.Args, "--")
	assert.Equal(t, expectedShell, cmd.Args[len(cmd.Args)-3])
	assert.Equal(t, "-c", cmd.Args[len(cmd.Args)-2])
	assert.Equal(t, "echo hello", cmd.Args[len(cmd.Args)-1])
}

func TestShellToolExecuteInvokesSandbox(t *testing.T) {
	expectedShell, err := exec.LookPath("sh")
	require.NoError(t, err)
	sb := &recordingSandbox{}
	st := NewShellTool(t.TempDir(), 30*time.Second)
	st.SetSandbox(sb)

	input, _ := json.Marshal(map[string]string{"command": "echo hello"})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.True(t, sb.called)
	assert.Equal(t, expectedShell, sb.path)
	assert.Equal(t, []string{"sh", "-c", "echo hello"}, sb.args)
}

func TestBubblewrapSandboxRejectsDisabledSubprocesses(t *testing.T) {
	sb := &bubblewrapSandbox{
		binary: "/usr/bin/bwrap",
		policy: ShellSandboxPolicy{
			AllowSubprocs: false,
		},
	}

	err := sb.Wrap(exec.Command("sh", "-c", "echo hello"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "subprocesses disabled by policy")
}

func TestDefaultShellSandboxPolicyKeepsWritesInsideWorkspaceAndTemp(t *testing.T) {
	workDir := t.TempDir()

	policy := DefaultShellSandboxPolicy(workDir)

	assert.Contains(t, policy.WritablePaths, workDir)
	assert.Contains(t, policy.WritablePaths, filepath.Clean(os.TempDir()))
	for _, path := range policy.WritablePaths {
		assert.False(t, strings.Contains(path, ".cache"))
		assert.False(t, strings.Contains(path, ".config"))
		assert.False(t, strings.Contains(path, "Library/Caches"))
		assert.False(t, strings.Contains(path, "Library/Application Support"))
	}
}

func TestShellToolExecuteReturnsSandboxSetupError(t *testing.T) {
	st := NewShellTool(t.TempDir(), 30*time.Second)
	st.SetSandbox(&recordingSandbox{err: errors.New("boom")})

	input, _ := json.Marshal(map[string]string{"command": "echo hello"})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "sandbox:")
	assert.Contains(t, result.Content, "boom")
}

func TestSandboxBackendAvailableLinuxProbe(t *testing.T) {
	assert.False(t, sandboxBackendAvailable("linux", "/nonexistent/bwrap", t.TempDir()))
}

func TestSeatbeltProfileWithProxyPort(t *testing.T) {
	policy := ShellSandboxPolicy{
		AllowedPaths:  []string{"/bin"},
		WritablePaths: []string{"/tmp/work"},
		AllowSubprocs: true,
		ProxyPort:     12345,
	}

	profile := buildSeatbeltProfile(policy)

	assert.Contains(t, profile, "network-outbound")
	assert.Contains(t, profile, "localhost:12345")
	assert.NotContains(t, profile, "(deny network*)")
}

func TestSeatbeltProfileNoNetwork(t *testing.T) {
	policy := ShellSandboxPolicy{
		AllowedPaths:  []string{"/bin"},
		WritablePaths: []string{"/tmp/work"},
		AllowSubprocs: true,
		ProxyPort:     0,
	}

	profile := buildSeatbeltProfile(policy)

	assert.NotContains(t, profile, "network-outbound")
	assert.Contains(t, profile, "(deny default)")
}

func TestSandboxCommandEnvProxyVars(t *testing.T) {
	cmd := exec.Command("sh", "-c", "echo hello")
	cmd.Dir = t.TempDir()

	env := sandboxCommandEnv(cmd, 8080)

	envMap := make(map[string]string)
	for _, entry := range env {
		k, v, ok := strings.Cut(entry, "=")
		if ok {
			envMap[k] = v
		}
	}

	assert.Equal(t, "http://127.0.0.1:8080", envMap["HTTP_PROXY"])
	assert.Equal(t, "http://127.0.0.1:8080", envMap["HTTPS_PROXY"])
	assert.Equal(t, "http://127.0.0.1:8080", envMap["http_proxy"])
	assert.Equal(t, "http://127.0.0.1:8080", envMap["https_proxy"])
	assert.Equal(t, "localhost,127.0.0.1", envMap["NO_PROXY"])
	assert.Equal(t, "localhost,127.0.0.1", envMap["no_proxy"])
}

func TestSandboxCommandEnvNoProxy(t *testing.T) {
	cmd := exec.Command("sh", "-c", "echo hello")
	cmd.Dir = t.TempDir()

	env := sandboxCommandEnv(cmd, 0)

	envMap := make(map[string]string)
	for _, entry := range env {
		k, v, ok := strings.Cut(entry, "=")
		if ok {
			envMap[k] = v
		}
	}

	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy", "NO_PROXY", "no_proxy"} {
		_, has := envMap[key]
		assert.False(t, has, "%s should not be set when proxyPort is 0", key)
	}
}

func TestBuildSandboxPolicyMergesConfig(t *testing.T) {
	cfg := config.SandboxConfig{
		Filesystem: config.SandboxFilesystemConfig{
			AllowWrite: []string{"/opt/custom"},
			DenyRead:   []string{"/etc/secrets"},
		},
		Network: config.SandboxNetworkConfig{ProxyPort: 9999},
	}
	policy := BuildSandboxPolicy("/project", cfg)

	assert.Contains(t, policy.WritablePaths, filepath.Clean("/project"))
	assert.Contains(t, policy.WritablePaths, "/opt/custom")
	assert.Contains(t, policy.DeniedPaths, "/etc/secrets")
	assert.Equal(t, 9999, policy.ProxyPort)
}

func TestBuildSandboxPolicyDefaults(t *testing.T) {
	policy := BuildSandboxPolicy("/project", config.SandboxConfig{})
	assert.Contains(t, policy.WritablePaths, filepath.Clean("/project"))
	assert.Equal(t, 0, policy.ProxyPort)
	assert.Empty(t, policy.DeniedPaths)
}

func TestIsExcludedFromSandbox(t *testing.T) {
	tests := []struct {
		cmd      string
		excluded []string
		want     bool
	}{
		{"docker build .", []string{"docker"}, true},
		{"sudo docker run", []string{"docker"}, true},
		{"dockerfile-lint .", []string{"docker"}, false},
		{"echo hello", []string{"docker"}, false},
		{"docker build . | grep err", []string{"docker"}, true},
		{"echo hi", nil, false},
		{"", []string{"docker"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			assert.Equal(t, tt.want, IsExcludedFromSandbox(tt.cmd, tt.excluded))
		})
	}
}

func TestShellToolExcludedCommandBypassesSandbox(t *testing.T) {
	st := NewShellTool(t.TempDir(), 30*time.Second)
	recorder := &recordingSandbox{}
	st.SetSandbox(recorder)
	st.SetSandboxConfig(config.SandboxConfig{
		ExcludedCommands: []string{"echo"},
	}, nil)

	input, _ := json.Marshal(shellInput{Command: "echo hello"})
	result, err := st.ExecuteStream(context.Background(), input, nil)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.False(t, recorder.called, "sandbox should not wrap excluded commands")
}

func TestShellToolHardLockdownBlocksFallback(t *testing.T) {
	st := NewShellTool(t.TempDir(), 30*time.Second)
	st.SetSandbox(&recordingSandbox{err: errors.New("sandbox unavailable")})
	f := false
	st.SetSandboxConfig(config.SandboxConfig{
		AllowUnsandboxedCommands: &f,
	}, nil)

	input, _ := json.Marshal(shellInput{Command: "echo hello"})
	result, err := st.ExecuteStream(context.Background(), input, nil)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unsandboxed execution is disabled")
}

func TestShellToolExcludedSudoCommand(t *testing.T) {
	st := NewShellTool(t.TempDir(), 30*time.Second)
	recorder := &recordingSandbox{}
	st.SetSandbox(recorder)
	st.SetSandboxConfig(config.SandboxConfig{
		ExcludedCommands: []string{"echo"},
	}, nil)

	input, _ := json.Marshal(shellInput{Command: "sudo echo hello"})
	result, err := st.ExecuteStream(context.Background(), input, nil)
	require.NoError(t, err)
	assert.False(t, recorder.called, "sudo echo should match excluded 'echo'")
	_ = result
}

type recordingSandbox struct {
	called bool
	path   string
	args   []string
	err    error
}

func (s *recordingSandbox) Name() string {
	return "recording"
}

func (s *recordingSandbox) Wrap(cmd *exec.Cmd) error {
	resolvedPath, _, resolveErr := resolveWrappedCommand(cmd)
	if resolveErr != nil {
		return resolveErr
	}
	s.called = true
	s.path = resolvedPath
	s.args = append([]string(nil), cmd.Args...)
	return s.err
}
