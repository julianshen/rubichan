package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectShellSandboxDarwinUsesSeatbeltWhenAvailable(t *testing.T) {
	sb := selectShellSandbox("darwin", t.TempDir(), func(file string) (string, error) {
		if file == "sandbox-exec" {
			return "/usr/bin/sandbox-exec", nil
		}
		return "", errors.New("unexpected lookup")
	})

	require.NotNil(t, sb)
	assert.Equal(t, "seatbelt", sb.Name())
}

func TestSelectShellSandboxLinuxUsesBubblewrapWhenAvailable(t *testing.T) {
	sb := selectShellSandbox("linux", t.TempDir(), func(file string) (string, error) {
		if file == "bwrap" {
			return "/usr/bin/bwrap", nil
		}
		return "", errors.New("unexpected lookup")
	})

	require.NotNil(t, sb)
	assert.Equal(t, "bubblewrap", sb.Name())
}

func TestSelectShellSandboxFallsBackWhenBackendMissing(t *testing.T) {
	sb := selectShellSandbox("linux", t.TempDir(), func(string) (string, error) {
		return "", errors.New("missing")
	})

	assert.Nil(t, sb)
}

func TestSeatbeltSandboxWrapsCommand(t *testing.T) {
	sb := &seatbeltSandbox{
		binary: "/usr/bin/sandbox-exec",
		policy: ShellSandboxPolicy{
			AllowedPaths:  []string{"/bin"},
			WritablePaths: []string{"/tmp/work"},
			AllowSubprocs: true,
		},
	}
	cmd := exec.Command("sh", "-c", "echo hello")

	err := sb.Wrap(cmd)
	require.NoError(t, err)
	assert.Equal(t, "/usr/bin/sandbox-exec", cmd.Path)
	assert.Equal(t, "/usr/bin/sandbox-exec", cmd.Args[0])
	assert.Contains(t, cmd.Args, "/bin/sh")
	assert.Contains(t, cmd.Args[2], "(deny default)")
	assert.Contains(t, cmd.Args[2], `(allow file-write* (subpath "/tmp/work"))`)
}

func TestBubblewrapSandboxWrapsCommand(t *testing.T) {
	workDir := t.TempDir()
	sb := &bubblewrapSandbox{
		binary: "/usr/bin/bwrap",
		policy: ShellSandboxPolicy{
			AllowedPaths:  []string{"/bin"},
			WritablePaths: []string{workDir},
		},
	}
	cmd := exec.Command("sh", "-c", "echo hello")
	cmd.Dir = workDir

	err := sb.Wrap(cmd)
	require.NoError(t, err)
	assert.Equal(t, "/usr/bin/bwrap", cmd.Path)
	assert.Equal(t, "/usr/bin/bwrap", cmd.Args[0])
	assert.Contains(t, cmd.Args, "--unshare-all")
	assert.Contains(t, cmd.Args, "--chdir")
	assert.Contains(t, cmd.Args, workDir)
	assert.Contains(t, cmd.Args, "--")
	assert.Equal(t, "/bin/sh", cmd.Args[len(cmd.Args)-3])
	assert.Equal(t, "-c", cmd.Args[len(cmd.Args)-2])
	assert.Equal(t, "echo hello", cmd.Args[len(cmd.Args)-1])
}

func TestShellToolExecuteInvokesSandbox(t *testing.T) {
	sb := &recordingSandbox{}
	st := NewShellTool(t.TempDir(), 30*time.Second)
	st.SetSandbox(sb)

	input, _ := json.Marshal(map[string]string{"command": "echo hello"})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.True(t, sb.called)
	assert.Equal(t, "/bin/sh", sb.path)
	assert.Equal(t, []string{"sh", "-c", "echo hello"}, sb.args)
}

func TestShellToolExecuteReturnsSandboxSetupError(t *testing.T) {
	st := NewShellTool(t.TempDir(), 30*time.Second)
	st.SetSandbox(&recordingSandbox{err: errors.New("boom")})

	input, _ := json.Marshal(map[string]string{"command": "echo hello"})
	result, err := st.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "sandbox setup failed")
	assert.Contains(t, result.Content, "boom")
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
	s.called = true
	s.path = cmd.Path
	s.args = append([]string(nil), cmd.Args...)
	return s.err
}
