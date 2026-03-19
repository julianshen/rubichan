package lsp

import (
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTryInstallSkipsMissingPrereq(t *testing.T) {
	r := NewRegistry()
	m := NewManager(r, t.TempDir(), true)

	r.Register(ServerConfig{
		Language: "fake-lang", Command: "fake-server", Extensions: []string{".fake"},
		InstallCmds: []InstallCmd{{Method: "nonexistent-tool-xyz", Command: "nonexistent-tool-xyz install fake-server"}},
	})

	err := m.TryInstall(context.Background(), "fake-lang")
	assert.Error(t, err)
}

func TestTryInstallNoCommands(t *testing.T) {
	r := NewRegistry()
	m := NewManager(r, t.TempDir(), true)
	r.Register(ServerConfig{Language: "no-install", Command: "no-server", Extensions: []string{".nope"}})

	err := m.TryInstall(context.Background(), "no-install")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no install commands")
}

func TestTryInstallWithHintOnly(t *testing.T) {
	r := NewRegistry()
	m := NewManager(r, t.TempDir(), true)
	r.Register(ServerConfig{
		Language: "hint-only", Command: "hint-server", Extensions: []string{".hint"},
		InstallCmds: []InstallCmd{{Hint: "Please install manually"}},
	})

	err := m.TryInstall(context.Background(), "hint-only")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Please install manually")
}

func TestTryInstallCachesAttempts(t *testing.T) {
	r := NewRegistry()
	m := NewManager(r, t.TempDir(), true)
	r.Register(ServerConfig{
		Language: "cached", Command: "cached-server", Extensions: []string{".cache"},
		InstallCmds: []InstallCmd{{Method: "nonexistent-xyz", Command: "nonexistent-xyz install"}},
	})

	err1 := m.TryInstall(context.Background(), "cached")
	assert.Error(t, err1)

	err2 := m.TryInstall(context.Background(), "cached")
	assert.Error(t, err2)
	assert.Contains(t, err2.Error(), "already attempted")
}

func TestTryInstallSucceedsWithEcho(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}

	r := NewRegistry()
	m := NewManager(r, t.TempDir(), true)
	r.Register(ServerConfig{
		Language: "echo-lang", Command: "echo-server", Extensions: []string{".echo"},
		InstallCmds: []InstallCmd{{Method: "", Command: "echo installed"}},
	})

	err := m.TryInstall(context.Background(), "echo-lang")
	assert.NoError(t, err)
}

func TestTryInstallUnknownLanguage(t *testing.T) {
	r := NewRegistry()
	m := NewManager(r, t.TempDir(), true)

	err := m.TryInstall(context.Background(), "unknown-lang")
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrNoConfig)
}
