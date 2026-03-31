package shell

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- 3A: Platform Detection ---

func TestDetectPackageManagerBrew(t *testing.T) {
	t.Parallel()

	lookup := func(name string) bool {
		return name == "brew"
	}
	pm := detectPackageManagerWith(lookup)
	require.NotNil(t, pm)
	assert.Equal(t, "brew", pm.Name)
	assert.Equal(t, "brew install", pm.InstallCmd)
}

func TestDetectPackageManagerApt(t *testing.T) {
	t.Parallel()

	lookup := func(name string) bool {
		return name == "apt"
	}
	pm := detectPackageManagerWith(lookup)
	require.NotNil(t, pm)
	assert.Equal(t, "apt", pm.Name)
	assert.Equal(t, "sudo apt install -y", pm.InstallCmd)
}

func TestDetectPackageManagerNone(t *testing.T) {
	t.Parallel()

	lookup := func(_ string) bool { return false }
	pm := detectPackageManagerWith(lookup)
	assert.Nil(t, pm)
}

func TestDetectPackageManagerPriority(t *testing.T) {
	t.Parallel()

	// Both brew and apt available — brew should win
	lookup := func(name string) bool {
		return name == "brew" || name == "apt"
	}
	pm := detectPackageManagerWith(lookup)
	require.NotNil(t, pm)
	assert.Equal(t, "brew", pm.Name)
}

// --- 3B: Command-Not-Found Detection ---

func TestIsCommandNotFound_ExitCode127(t *testing.T) {
	t.Parallel()
	assert.True(t, IsCommandNotFound("anything", 127))
}

func TestIsCommandNotFound_StderrPattern(t *testing.T) {
	t.Parallel()
	assert.True(t, IsCommandNotFound("bash: jq: command not found", 1))
	assert.True(t, IsCommandNotFound("zsh: command not found: jq", 1))
	assert.True(t, IsCommandNotFound("jq: not found", 127))
}

func TestIsCommandNotFound_NormalFailure(t *testing.T) {
	t.Parallel()
	assert.False(t, IsCommandNotFound("test failed", 1))
	assert.False(t, IsCommandNotFound("compilation error", 2))
	assert.False(t, IsCommandNotFound("", 1))
}

// --- 3C: Built-in Lookup Table ---

func TestBuiltinLookupKnownTool(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "jq", LookupPackage("jq", "brew"))
	assert.Equal(t, "jq", LookupPackage("jq", "apt"))
	assert.Equal(t, "ripgrep", LookupPackage("rg", "apt"))
	assert.Equal(t, "ripgrep", LookupPackage("rg", "brew"))
}

func TestBuiltinLookupUnknownTool(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", LookupPackage("obscuretool123", "brew"))
}

func TestBuiltinLookupPlatformVariants(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "fd", LookupPackage("fd", "brew"))
	assert.Equal(t, "fd-find", LookupPackage("fd", "apt"))
}

// --- 3D: PackageInstaller ---

func TestPackageInstallerUsesBuiltinFirst(t *testing.T) {
	t.Parallel()

	llmCalled := false
	agentTurn := func(_ context.Context, _ string) (<-chan TurnEvent, error) {
		llmCalled = true
		ch := make(chan TurnEvent, 1)
		close(ch)
		return ch, nil
	}

	pkgSearchCalled := false
	pkgSearch := func(_ context.Context, _ string, _ string) (string, error) {
		pkgSearchCalled = true
		return "", nil
	}

	pi := NewPackageInstaller(
		&PackageManager{Name: "brew", InstallCmd: "brew install"},
		agentTurn,
		nil, nil, // shellExec, approvalFn not needed for this test
	)
	pi.pkgSearch = pkgSearch

	pkg := pi.resolvePackage(context.Background(), "jq")
	assert.Equal(t, "jq", pkg)
	assert.False(t, llmCalled, "LLM should not be called for built-in lookup")
	assert.False(t, pkgSearchCalled, "package search should not be called for built-in lookup")
}

func TestPackageInstallerFallsThroughToPkgSearch(t *testing.T) {
	t.Parallel()

	llmCalled := false
	agentTurn := func(_ context.Context, _ string) (<-chan TurnEvent, error) {
		llmCalled = true
		ch := make(chan TurnEvent, 1)
		close(ch)
		return ch, nil
	}

	pi := NewPackageInstaller(
		&PackageManager{Name: "brew", InstallCmd: "brew install"},
		agentTurn,
		nil, nil,
	)
	pi.pkgSearch = func(_ context.Context, _ string, _ string) (string, error) {
		return "myobscuretool-pkg", nil
	}

	pkg := pi.resolvePackage(context.Background(), "myobscuretool")
	assert.Equal(t, "myobscuretool-pkg", pkg)
	assert.False(t, llmCalled, "LLM should not be called when pkg search succeeds")
}

func TestPackageInstallerFallsThroughToLLM(t *testing.T) {
	t.Parallel()

	agentTurn := func(_ context.Context, _ string) (<-chan TurnEvent, error) {
		ch := make(chan TurnEvent, 2)
		ch <- TurnEvent{Type: "text_delta", Text: "exotic-package"}
		ch <- TurnEvent{Type: "done"}
		close(ch)
		return ch, nil
	}

	pi := NewPackageInstaller(
		&PackageManager{Name: "brew", InstallCmd: "brew install"},
		agentTurn,
		nil, nil,
	)
	pi.pkgSearch = func(_ context.Context, _ string, _ string) (string, error) {
		return "", nil // not found
	}

	pkg := pi.resolvePackage(context.Background(), "exotictool")
	assert.Equal(t, "exotic-package", pkg)
}

func TestPackageInstallerApprovalRequired(t *testing.T) {
	t.Parallel()

	installCalled := false
	exec := func(_ context.Context, cmd string, _ string) (string, string, int, error) {
		installCalled = true
		return "", "", 0, nil
	}

	// Rejection
	approvalFn := func(_ context.Context, _ string) (bool, error) {
		return false, nil
	}

	pi := NewPackageInstaller(
		&PackageManager{Name: "brew", InstallCmd: "brew install"},
		nil, exec, approvalFn,
	)

	handled, err := pi.HandleCommandNotFound(context.Background(), "jq '.name' file.json", "command not found: jq", 127, &bytes.Buffer{}, &bytes.Buffer{})
	assert.NoError(t, err)
	assert.True(t, handled)
	assert.False(t, installCalled, "install should not run when user rejects")
}

func TestPackageInstallerRerunsOnSuccess(t *testing.T) {
	t.Parallel()

	var commands []string
	exec := func(_ context.Context, cmd string, _ string) (string, string, int, error) {
		commands = append(commands, cmd)
		return "success", "", 0, nil
	}

	approvalFn := func(_ context.Context, _ string) (bool, error) {
		return true, nil
	}

	pi := NewPackageInstaller(
		&PackageManager{Name: "brew", InstallCmd: "brew install"},
		nil, exec, approvalFn,
	)

	stdout := &bytes.Buffer{}
	handled, err := pi.HandleCommandNotFound(context.Background(), "jq '.name' file.json", "command not found: jq", 127, stdout, &bytes.Buffer{})
	assert.NoError(t, err)
	assert.True(t, handled)

	// Should have: install command + re-run of original command
	require.Len(t, commands, 2)
	assert.Contains(t, commands[0], "brew install jq")
	assert.Equal(t, "jq '.name' file.json", commands[1])
}

func TestPackageInstallerCachesMapping(t *testing.T) {
	t.Parallel()

	llmCalls := 0
	agentTurn := func(_ context.Context, _ string) (<-chan TurnEvent, error) {
		llmCalls++
		ch := make(chan TurnEvent, 2)
		ch <- TurnEvent{Type: "text_delta", Text: "ripgrep"}
		ch <- TurnEvent{Type: "done"}
		close(ch)
		return ch, nil
	}

	pi := NewPackageInstaller(
		&PackageManager{Name: "apt", InstallCmd: "sudo apt install -y"},
		agentTurn, nil, nil,
	)
	pi.pkgSearch = func(_ context.Context, _ string, _ string) (string, error) {
		return "", nil // force LLM fallback
	}

	// First call — built-in table finds it for apt as "ripgrep"
	pkg := pi.resolvePackage(context.Background(), "rg")
	assert.Equal(t, "ripgrep", pkg)

	// No LLM calls since "rg" is in the built-in table for apt
	assert.Equal(t, 0, llmCalls)
}

func TestPackageInstallerNilSafe(t *testing.T) {
	t.Parallel()

	// nil pkgInstaller on ShellHost should not panic
	exec := func(_ context.Context, _ string, _ string) (string, string, int, error) {
		return "", "command not found: jq", 127, nil
	}

	host, _, stderr := newTestHost("echo test\n", exec, nil)
	err := host.Run(context.Background())
	assert.NoError(t, err)
	// Should not contain any install prompt — just normal error
	assert.NotContains(t, stderr.String(), "Install")
}

func TestPackageInstallerNoPkgManager(t *testing.T) {
	t.Parallel()

	// No package manager — should suggest but not offer to install
	pi := NewPackageInstaller(nil, nil, nil, nil)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	handled, err := pi.HandleCommandNotFound(context.Background(), "jq '.name'", "command not found: jq", 127, stdout, stderr)
	assert.NoError(t, err)
	assert.False(t, handled, "should not handle when no package manager")
}

func TestShellHostPackageInstallerIntegration(t *testing.T) {
	t.Parallel()

	var commands []string
	exec := func(_ context.Context, cmd string, _ string) (string, string, int, error) {
		commands = append(commands, cmd)
		if cmd == "jq '.name' file.json" && len(commands) == 1 {
			return "", "bash: jq: command not found", 127, nil
		}
		if strings.Contains(cmd, "brew install") {
			return "", "", 0, nil
		}
		// Re-run after install
		return `"rubichan"`, "", 0, nil
	}

	approvalFn := func(_ context.Context, _ string) (bool, error) {
		return true, nil
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	host := NewShellHost(ShellHostConfig{
		WorkDir:     "/project",
		HomeDir:     "/home/user",
		Executables: map[string]bool{"jq": true},
		ShellExec:   exec,
		Stdin:       strings.NewReader("jq '.name' file.json\n"),
		Stdout:      stdout,
		Stderr:      stderr,
		GitBranchFn: func(string) string { return "" },
		PackageManager: &PackageManager{
			Name:       "brew",
			InstallCmd: "brew install",
		},
		InstallApprovalFn: approvalFn,
	})

	err := host.Run(context.Background())
	assert.NoError(t, err)

	// Should have: original command, install, re-run
	require.GreaterOrEqual(t, len(commands), 3)
	assert.Equal(t, "jq '.name' file.json", commands[0])
	assert.Contains(t, commands[1], "brew install")
	assert.Equal(t, "jq '.name' file.json", commands[2])

	// Re-run output should be shown
	assert.Contains(t, stdout.String(), `"rubichan"`)
}
