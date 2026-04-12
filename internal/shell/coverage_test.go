package shell

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- shell.go edge cases ---

// TestShellHost_NilShellExec exercises the "shell execution not available" branch.
func TestShellHost_NilShellExec(t *testing.T) {
	t.Parallel()

	host, _, stderr := newTestHost("echo hi\n", nil, nil)
	err := host.Run(context.Background())
	assert.NoError(t, err)
	assert.Contains(t, stderr.String(), "shell execution not available")
}

// TestShellHost_ShellExecError covers the "error: %v" branch in handleShellCommand.
func TestShellHost_ShellExecError(t *testing.T) {
	t.Parallel()

	exec := func(_ context.Context, _ string, _ string) (string, string, int, error) {
		return "", "", 0, errors.New("pty failed")
	}
	host, _, stderr := newTestHost("echo hi\n", exec, nil)
	err := host.Run(context.Background())
	assert.NoError(t, err)
	assert.Contains(t, stderr.String(), "pty failed")
}

// TestShellHost_NilAgentOnQuery covers the "agent not available" branch.
func TestShellHost_NilAgentOnQuery(t *testing.T) {
	t.Parallel()

	host, _, stderr := newTestHost("explain the code\n", nil, nil)
	err := host.Run(context.Background())
	assert.NoError(t, err)
	assert.Contains(t, stderr.String(), "agent not available")
}

// TestShellHost_AgentTurnError covers the agent error branch.
func TestShellHost_AgentTurnError(t *testing.T) {
	t.Parallel()

	agent := func(_ context.Context, _ string) (<-chan TurnEvent, error) {
		return nil, errors.New("upstream down")
	}
	host, _, stderr := newTestHost("explain\n", nil, agent)
	err := host.Run(context.Background())
	assert.NoError(t, err)
	assert.Contains(t, stderr.String(), "upstream down")
}

// TestShellHost_HandleCDNoArgs covers the early return when `cd` has no args.
func TestShellHost_HandleCDNoArgs(t *testing.T) {
	t.Parallel()

	host, _, stderr := newTestHost("cd\n", nil, nil)
	err := host.Run(context.Background())
	assert.NoError(t, err)
	// Nothing was written
	assert.Empty(t, stderr.String())
	assert.Equal(t, "/project", host.workDir)
}

// TestShellHost_SlashCommandNil covers the "command not available" branch.
func TestShellHost_SlashCommandNil(t *testing.T) {
	t.Parallel()

	host, _, stderr := newTestHost("/model foo\n", nil, nil)
	err := host.Run(context.Background())
	assert.NoError(t, err)
	assert.Contains(t, stderr.String(), "command not available")
}

// TestShellHost_SlashCommandError covers the slash handler error path.
func TestShellHost_SlashCommandError(t *testing.T) {
	t.Parallel()

	slashFn := func(_ context.Context, _ string, _ []string) (string, bool, error) {
		return "", false, errors.New("boom")
	}
	host := NewShellHost(ShellHostConfig{
		WorkDir:        "/project",
		HomeDir:        "/home/user",
		Executables:    map[string]bool{},
		SlashCommandFn: slashFn,
		Stdin:          strings.NewReader("/broken\n"),
		Stdout:         &bytes.Buffer{},
		Stderr:         &bytes.Buffer{},
		GitBranchFn:    func(string) string { return "" },
	})
	err := host.Run(context.Background())
	assert.NoError(t, err)
}

// TestShellHost_StreamEventsErrorEvent covers the error-event branch of streamEvents.
func TestShellHost_StreamEventsErrorEvent(t *testing.T) {
	t.Parallel()

	agent := func(_ context.Context, _ string) (<-chan TurnEvent, error) {
		ch := make(chan TurnEvent, 2)
		ch <- TurnEvent{Type: EventError, Text: "bad things"}
		ch <- TurnEvent{Type: EventDone}
		close(ch)
		return ch, nil
	}
	host, _, stderr := newTestHost("describe this\n", nil, agent)
	err := host.Run(context.Background())
	assert.NoError(t, err)
	assert.Contains(t, stderr.String(), "bad things")
}

// TestShellHost_HandleSmartScriptGenerationError covers the script-generation error branch.
func TestShellHost_HandleSmartScriptGenerationError(t *testing.T) {
	t.Parallel()

	callCount := 0
	agent := func(_ context.Context, _ string) (<-chan TurnEvent, error) {
		callCount++
		if callCount == 1 {
			ch := make(chan TurnEvent, 2)
			ch <- TurnEvent{Type: EventTextDelta, Text: "action"}
			ch <- TurnEvent{Type: EventDone}
			close(ch)
			return ch, nil
		}
		return nil, errors.New("generator broken")
	}

	approvalFn := func(_ context.Context, _ string) (bool, string, error) {
		return true, "", nil
	}
	host := NewShellHost(ShellHostConfig{
		WorkDir:          "/project",
		HomeDir:          "/home/user",
		Executables:      map[string]bool{},
		AgentTurn:        agent,
		Stdin:            strings.NewReader("? do something\n"),
		Stdout:           &bytes.Buffer{},
		Stderr:           &bytes.Buffer{},
		GitBranchFn:      func(string) string { return "" },
		ScriptApprovalFn: approvalFn,
	})
	err := host.Run(context.Background())
	assert.NoError(t, err)
}

// TestShellHost_HandleSmartScriptRejected covers the rejection branch.
func TestShellHost_HandleSmartScriptRejected(t *testing.T) {
	t.Parallel()

	callCount := 0
	agent := func(_ context.Context, _ string) (<-chan TurnEvent, error) {
		callCount++
		ch := make(chan TurnEvent, 2)
		if callCount == 1 {
			ch <- TurnEvent{Type: EventTextDelta, Text: "action"}
		} else {
			ch <- TurnEvent{Type: EventTextDelta, Text: "#!/bin/sh\necho hi"}
		}
		ch <- TurnEvent{Type: EventDone}
		close(ch)
		return ch, nil
	}

	approvalFn := func(_ context.Context, _ string) (bool, string, error) {
		return false, "", nil
	}

	stderr := &bytes.Buffer{}
	host := NewShellHost(ShellHostConfig{
		WorkDir:          "/project",
		HomeDir:          "/home/user",
		Executables:      map[string]bool{},
		AgentTurn:        agent,
		Stdin:            strings.NewReader("? do something\n"),
		Stdout:           &bytes.Buffer{},
		Stderr:           stderr,
		GitBranchFn:      func(string) string { return "" },
		ScriptApprovalFn: approvalFn,
	})
	err := host.Run(context.Background())
	assert.NoError(t, err)
	assert.Contains(t, stderr.String(), "discarded")
}

// TestShellHost_HandleSmartScriptApprovalError covers the approval error branch.
func TestShellHost_HandleSmartScriptApprovalError(t *testing.T) {
	t.Parallel()

	callCount := 0
	agent := func(_ context.Context, _ string) (<-chan TurnEvent, error) {
		callCount++
		ch := make(chan TurnEvent, 2)
		if callCount == 1 {
			ch <- TurnEvent{Type: EventTextDelta, Text: "action"}
		} else {
			ch <- TurnEvent{Type: EventTextDelta, Text: "echo hi"}
		}
		ch <- TurnEvent{Type: EventDone}
		close(ch)
		return ch, nil
	}

	approvalFn := func(_ context.Context, _ string) (bool, string, error) {
		return false, "", errors.New("approval busted")
	}
	host := NewShellHost(ShellHostConfig{
		WorkDir:          "/project",
		HomeDir:          "/home/user",
		Executables:      map[string]bool{},
		AgentTurn:        agent,
		Stdin:            strings.NewReader("? something\n"),
		Stdout:           &bytes.Buffer{},
		Stderr:           &bytes.Buffer{},
		GitBranchFn:      func(string) string { return "" },
		ScriptApprovalFn: approvalFn,
	})
	err := host.Run(context.Background())
	assert.NoError(t, err)
}

// TestShellHost_HandleSmartScriptExecuteError covers the execute error branch.
func TestShellHost_HandleSmartScriptExecuteError(t *testing.T) {
	t.Parallel()

	callCount := 0
	agent := func(_ context.Context, _ string) (<-chan TurnEvent, error) {
		callCount++
		ch := make(chan TurnEvent, 2)
		if callCount == 1 {
			ch <- TurnEvent{Type: EventTextDelta, Text: "action"}
		} else {
			ch <- TurnEvent{Type: EventTextDelta, Text: "echo hi"}
		}
		ch <- TurnEvent{Type: EventDone}
		close(ch)
		return ch, nil
	}

	exec := func(_ context.Context, _ string, _ string) (string, string, int, error) {
		return "", "", 1, errors.New("exec failed")
	}
	approvalFn := func(_ context.Context, _ string) (bool, string, error) {
		return true, "", nil
	}
	host := NewShellHost(ShellHostConfig{
		WorkDir:          "/project",
		HomeDir:          "/home/user",
		Executables:      map[string]bool{},
		AgentTurn:        agent,
		ShellExec:        exec,
		Stdin:            strings.NewReader("? something\n"),
		Stdout:           &bytes.Buffer{},
		Stderr:           &bytes.Buffer{},
		GitBranchFn:      func(string) string { return "" },
		ScriptApprovalFn: approvalFn,
	})
	err := host.Run(context.Background())
	assert.NoError(t, err)
}

// TestShellHost_HandleSmartScriptUsesEditedScript covers the edited-script branch.
func TestShellHost_HandleSmartScriptUsesEditedScript(t *testing.T) {
	t.Parallel()

	callCount := 0
	agent := func(_ context.Context, _ string) (<-chan TurnEvent, error) {
		callCount++
		ch := make(chan TurnEvent, 2)
		if callCount == 1 {
			ch <- TurnEvent{Type: EventTextDelta, Text: "action"}
		} else {
			ch <- TurnEvent{Type: EventTextDelta, Text: "echo original"}
		}
		ch <- TurnEvent{Type: EventDone}
		close(ch)
		return ch, nil
	}

	var executedScript string
	exec := func(_ context.Context, script string, _ string) (string, string, int, error) {
		executedScript = script
		return "done", "", 0, nil
	}
	approvalFn := func(_ context.Context, _ string) (bool, string, error) {
		return true, "echo edited", nil
	}
	host := NewShellHost(ShellHostConfig{
		WorkDir:          "/project",
		HomeDir:          "/home/user",
		Executables:      map[string]bool{},
		AgentTurn:        agent,
		ShellExec:        exec,
		Stdin:            strings.NewReader("? something\n"),
		Stdout:           &bytes.Buffer{},
		Stderr:           &bytes.Buffer{},
		GitBranchFn:      func(string) string { return "" },
		ScriptApprovalFn: approvalFn,
	})
	err := host.Run(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "echo edited", executedScript)
}

// TestShellHost_HandleShellCommandGitStatusUpdate covers the status-line + git branch branch.
func TestShellHost_HandleShellCommandGitStatusUpdate(t *testing.T) {
	t.Parallel()

	exec := func(_ context.Context, _ string, _ string) (string, string, int, error) {
		return "ok", "", 0, nil
	}

	host := NewShellHost(ShellHostConfig{
		WorkDir:     "/project",
		HomeDir:     "/home/user",
		Executables: map[string]bool{"git": true},
		ShellExec:   exec,
		Stdin:       strings.NewReader("git status\n"),
		Stdout:      &bytes.Buffer{},
		Stderr:      &bytes.Buffer{},
		GitBranchFn: func(string) string { return "main" },
		StatusLine:  true,
	})
	err := host.Run(context.Background())
	assert.NoError(t, err)
}

// TestShellHost_CDWithStatusLine covers the status-line update inside cd.
func TestShellHost_CDWithStatusLine(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	sub := filepath.Join(tmp, "sub")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	host := NewShellHost(ShellHostConfig{
		WorkDir:     tmp,
		HomeDir:     tmp,
		Executables: map[string]bool{},
		Stdin:       strings.NewReader("cd sub\n"),
		Stdout:      &bytes.Buffer{},
		Stderr:      &bytes.Buffer{},
		GitBranchFn: func(string) string { return "main" },
		StatusLine:  true,
	})
	err := host.Run(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, sub, host.workDir)
}

// --- classifier.go edge cases ---

// TestClassify_EmptySlashWithOnlyArgs covers the "len(parts) == 0" branch after "/".
func TestClassify_EmptySlashWithOnlyArgs(t *testing.T) {
	t.Parallel()
	c := NewInputClassifier(map[string]bool{})

	result := c.Classify("/    ")
	// Fields is empty after trimming — should be ClassEmpty.
	assert.Equal(t, ClassEmpty, result.Classification)
}

// TestExtractExecutableEmptyLine covers the "all-env-var assignments" branch.
func TestExtractExecutableEmptyLine(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", extractExecutable("FOO=bar"))
	assert.Equal(t, "", extractExecutable(""))
}

// --- completer.go edge cases ---

// TestCompleterSlashCommandNilRegistry covers the nil slash command registry.
func TestCompleterSlashCommandNilRegistry(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	c := NewCompleter(map[string]bool{}, &workDir, nil, nil)
	results := c.Complete("/x", 2)
	assert.Empty(t, results)
}

// TestCompleterGitBranchNilFn covers the nil gitBranches branch.
func TestCompleterGitBranchNilFn(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	c := NewCompleter(map[string]bool{"git": true}, &workDir, nil, nil)
	results := c.Complete("git checkout ma", 15)
	assert.Empty(t, results)
}

// TestCompleterFilePathNilWorkDir covers the nil-workDir branch.
func TestCompleterFilePathNilWorkDir(t *testing.T) {
	t.Parallel()
	c := NewCompleter(map[string]bool{"ls": true}, nil, nil, nil)
	results := c.Complete("ls fo", 5)
	assert.Empty(t, results)
}

// TestCompleterFilePathUnreadableDir covers the ReadDir error branch.
func TestCompleterFilePathUnreadableDir(t *testing.T) {
	t.Parallel()
	bogusDir := filepath.Join(t.TempDir(), "definitely-missing")
	c := NewCompleter(map[string]bool{"ls": true}, &bogusDir, nil, nil)
	results := c.Complete("ls foo", 6)
	assert.Empty(t, results)
}

// TestCompleterFilePathHiddenFilesShownWhenPrefixDot exercises the dot-prefix branch.
func TestCompleterFilePathHiddenFilesShownWhenPrefixDot(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workDir, ".hidden"), nil, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "visible.txt"), nil, 0o644))

	c := NewCompleter(map[string]bool{"cat": true}, &workDir, nil, nil)
	results := c.Complete("cat .h", 6)
	names := completionTexts(results)
	assert.Contains(t, names, ".hidden")
}

// TestCompleterFilePathAbsoluteDir walks the absolute-path branch.
func TestCompleterFilePathAbsoluteDir(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	sub := filepath.Join(workDir, "sub")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "file.txt"), nil, 0o644))

	c := NewCompleter(map[string]bool{"ls": true}, &workDir, nil, nil)
	results := c.Complete("ls "+sub+"/", len("ls "+sub+"/"))
	names := completionTexts(results)
	assert.Contains(t, names, "file.txt")
}

// --- context.go edge cases ---

// TestNewContextTrackerNegative ensures negative inputs are clamped to zero.
func TestNewContextTrackerNegative(t *testing.T) {
	t.Parallel()
	tracker := NewContextTracker(-1)
	assert.Equal(t, 0, tracker.maxOutputSize)
}

// --- history.go edge cases ---

// TestHistoryNegativeMaxSize ensures negative inputs are clamped to zero.
func TestHistoryNegativeMaxSize(t *testing.T) {
	t.Parallel()
	h := NewCommandHistory(-5)
	assert.Equal(t, 0, h.maxSize)
}

// TestHistoryLoadMissingFile ensures Load propagates read errors.
func TestHistoryLoadMissingFile(t *testing.T) {
	t.Parallel()
	h := NewCommandHistory(10)
	err := h.Load(filepath.Join(t.TempDir(), "nope.txt"))
	assert.Error(t, err)
}

// TestHistoryLoadRespectsMaxSize ensures Load truncates to maxSize.
func TestHistoryLoadRespectsMaxSize(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "history.txt")
	require.NoError(t, os.WriteFile(path, []byte("a\nb\nc\nd\ne\n"), 0o600))

	h := NewCommandHistory(3)
	require.NoError(t, h.Load(path))
	assert.Equal(t, []string{"c", "d", "e"}, h.Entries())
}

// TestHistoryPreviousEmpty ensures Previous returns false when empty.
func TestHistoryPreviousEmpty(t *testing.T) {
	t.Parallel()
	h := NewCommandHistory(10)
	_, ok := h.Previous()
	assert.False(t, ok)

	_, ok = h.Next()
	assert.False(t, ok)
}

// --- helpers.go edge cases ---

// TestShortenHomeEdgeCases exercises all branches of shortenHome.
func TestShortenHomeEdgeCases(t *testing.T) {
	t.Parallel()

	// Empty homeDir leaves path unchanged.
	assert.Equal(t, "/opt/x", shortenHome("/opt/x", ""))

	// Path == homeDir returns "~".
	assert.Equal(t, "~", shortenHome("/home/user", "/home/user"))

	// Path starts with homeDir but next char is not slash — unchanged.
	assert.Equal(t, "/home/username/project", shortenHome("/home/username/project", "/home/user"))

	// Path under homeDir.
	assert.Equal(t, "~/project", shortenHome("/home/user/project", "/home/user"))

	// Path not under homeDir.
	assert.Equal(t, "/opt/x", shortenHome("/opt/x", "/home/user"))
}

// TestTruncateRunesExactLimit covers the case when n >= rune count.
func TestTruncateRunesExactLimit(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "abc", truncateRunes("abc", 5))
	assert.Equal(t, "abc", truncateRunes("abc", 3))
}

// TestWriteOutputEmpty covers the empty-string early-return branch.
func TestWriteOutputEmpty(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	writeOutput(buf, "")
	assert.Empty(t, buf.String())
}

// TestCollectTurnTextIgnoresNonText verifies only text_delta events contribute.
func TestCollectTurnTextIgnoresNonText(t *testing.T) {
	t.Parallel()

	ch := make(chan TurnEvent, 4)
	ch <- TurnEvent{Type: EventToolCall, Text: "ignored"}
	ch <- TurnEvent{Type: EventTextDelta, Text: "kept"}
	ch <- TurnEvent{Type: EventDone}
	close(ch)

	assert.Equal(t, "kept", collectTurnText(ch))
}

// --- package_installer.go edge cases ---

// TestExtractCommandNameEnvPrefix covers the env-prefix branch.
func TestExtractCommandNameEnvPrefix(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "go", extractCommandName("GOFLAGS=-v go build"))
	assert.Equal(t, "", extractCommandName(""))
	assert.Equal(t, "-l", extractCommandName("-l")) // edge: dash is NOT env, returned as-is
}

// TestHandleCommandNotFoundUnrelatedStderr covers the "not command-not-found" branch.
func TestHandleCommandNotFoundUnrelatedStderr(t *testing.T) {
	t.Parallel()
	pi := NewPackageInstaller(&PackageManager{Name: "brew", InstallCmd: "brew install"}, nil, nil, nil)
	handled, err := pi.HandleCommandNotFound(context.Background(), "go test ./...", "FAIL: stuff", 1, "/project", &bytes.Buffer{}, &bytes.Buffer{})
	assert.NoError(t, err)
	assert.False(t, handled)
}

// TestHandleCommandNotFoundEmptyCommand covers the empty-command branch.
func TestHandleCommandNotFoundEmptyCommand(t *testing.T) {
	t.Parallel()
	pi := NewPackageInstaller(&PackageManager{Name: "brew", InstallCmd: "brew install"}, nil, nil, nil)
	handled, err := pi.HandleCommandNotFound(context.Background(), "", "command not found", 127, "/project", &bytes.Buffer{}, &bytes.Buffer{})
	assert.NoError(t, err)
	assert.False(t, handled)
}

// TestHandleCommandNotFoundUnknownPackage covers the "no package" branch.
func TestHandleCommandNotFoundUnknownPackage(t *testing.T) {
	t.Parallel()

	agentTurn := func(_ context.Context, _ string) (<-chan TurnEvent, error) {
		ch := make(chan TurnEvent, 1)
		close(ch) // LLM returned nothing
		return ch, nil
	}
	pi := NewPackageInstaller(&PackageManager{Name: "brew", InstallCmd: "brew install"}, agentTurn, nil, nil)
	pi.pkgSearch = func(_ context.Context, _ string, _ string) (string, error) { return "", nil }

	stderr := &bytes.Buffer{}
	handled, err := pi.HandleCommandNotFound(context.Background(), "exotictool", "command not found: exotictool", 127, "/project", &bytes.Buffer{}, stderr)
	assert.NoError(t, err)
	assert.True(t, handled)
	assert.Contains(t, stderr.String(), "Could not determine package")
}

// TestHandleCommandNotFoundUnsafePackageName covers the safePackageName rejection branch.
func TestHandleCommandNotFoundUnsafePackageName(t *testing.T) {
	t.Parallel()

	agentTurn := func(_ context.Context, _ string) (<-chan TurnEvent, error) {
		ch := make(chan TurnEvent, 2)
		ch <- TurnEvent{Type: EventTextDelta, Text: "bad; rm -rf /"}
		ch <- TurnEvent{Type: EventDone}
		close(ch)
		return ch, nil
	}
	pi := NewPackageInstaller(&PackageManager{Name: "brew", InstallCmd: "brew install"}, agentTurn, nil, nil)
	pi.pkgSearch = func(_ context.Context, _ string, _ string) (string, error) { return "", nil }

	stderr := &bytes.Buffer{}
	handled, err := pi.HandleCommandNotFound(context.Background(), "bogus", "command not found: bogus", 127, "/project", &bytes.Buffer{}, stderr)
	assert.NoError(t, err)
	assert.True(t, handled)
	assert.Contains(t, stderr.String(), "unsafe characters")
}

// TestHandleCommandNotFoundNoApproval covers the nil-approvalFn branch.
func TestHandleCommandNotFoundNoApproval(t *testing.T) {
	t.Parallel()
	pi := NewPackageInstaller(&PackageManager{Name: "brew", InstallCmd: "brew install"}, nil, nil, nil)
	handled, err := pi.HandleCommandNotFound(context.Background(), "jq '.'", "command not found: jq", 127, "/project", &bytes.Buffer{}, &bytes.Buffer{})
	assert.NoError(t, err)
	assert.True(t, handled)
}

// TestHandleCommandNotFoundApprovalErr covers the approval-error branch.
func TestHandleCommandNotFoundApprovalErr(t *testing.T) {
	t.Parallel()
	pi := NewPackageInstaller(&PackageManager{Name: "brew", InstallCmd: "brew install"}, nil, nil, func(_ context.Context, _ string) (bool, error) {
		return false, errors.New("ui error")
	})
	handled, err := pi.HandleCommandNotFound(context.Background(), "jq '.'", "command not found: jq", 127, "/project", &bytes.Buffer{}, &bytes.Buffer{})
	assert.Error(t, err)
	assert.True(t, handled)
}

// TestHandleCommandNotFoundNilShellExec covers the nil-shellExec branch after approval.
func TestHandleCommandNotFoundNilShellExec(t *testing.T) {
	t.Parallel()
	pi := NewPackageInstaller(&PackageManager{Name: "brew", InstallCmd: "brew install"}, nil, nil, func(_ context.Context, _ string) (bool, error) {
		return true, nil
	})
	handled, err := pi.HandleCommandNotFound(context.Background(), "jq '.'", "command not found: jq", 127, "/project", &bytes.Buffer{}, &bytes.Buffer{})
	assert.NoError(t, err)
	assert.True(t, handled)
}

// TestHandleCommandNotFoundInstallFails covers the install-error branch.
func TestHandleCommandNotFoundInstallFails(t *testing.T) {
	t.Parallel()
	exec := func(_ context.Context, _ string, _ string) (string, string, int, error) {
		return "", "", 0, errors.New("network down")
	}
	pi := NewPackageInstaller(&PackageManager{Name: "brew", InstallCmd: "brew install"}, nil, exec, func(_ context.Context, _ string) (bool, error) {
		return true, nil
	})
	handled, err := pi.HandleCommandNotFound(context.Background(), "jq '.'", "command not found: jq", 127, "/project", &bytes.Buffer{}, &bytes.Buffer{})
	assert.Error(t, err)
	assert.True(t, handled)
}

// TestHandleCommandNotFoundInstallNonZeroExit covers the non-zero install exit branch.
func TestHandleCommandNotFoundInstallNonZeroExit(t *testing.T) {
	t.Parallel()
	exec := func(_ context.Context, _ string, _ string) (string, string, int, error) {
		return "", "no such package", 1, nil
	}
	pi := NewPackageInstaller(&PackageManager{Name: "brew", InstallCmd: "brew install"}, nil, exec, func(_ context.Context, _ string) (bool, error) {
		return true, nil
	})
	stderr := &bytes.Buffer{}
	handled, err := pi.HandleCommandNotFound(context.Background(), "jq '.'", "command not found: jq", 127, "/project", &bytes.Buffer{}, stderr)
	assert.NoError(t, err)
	assert.True(t, handled)
	assert.Contains(t, stderr.String(), "Installation failed")
}

// TestResolvePackageLLMError ensures LLM errors fall through to empty.
func TestResolvePackageLLMError(t *testing.T) {
	t.Parallel()
	agentTurn := func(_ context.Context, _ string) (<-chan TurnEvent, error) {
		return nil, errors.New("llm down")
	}
	pi := NewPackageInstaller(&PackageManager{Name: "brew", InstallCmd: "brew install"}, agentTurn, nil, nil)
	pi.pkgSearch = func(_ context.Context, _ string, _ string) (string, error) { return "", nil }

	pkg := pi.resolvePackage(context.Background(), "exotictool")
	assert.Empty(t, pkg)
}

// --- status_line.go edge cases ---

// TestStatusLineNilReceiverMethods verifies the nil-receiver guards work.
func TestStatusLineNilReceiverMethods(t *testing.T) {
	t.Parallel()
	var sl *StatusLine

	sl.Update("branch", "main") // must not panic
	sl.UpdateCWD("/path")       // must not panic
	sl.UpdateExitCode(1)        // must not panic
	sl.UpdateModel("sonnet")    // must not panic
	assert.Empty(t, sl.Render())
}

// TestStatusLineDisabledUpdateCWD covers the disabled-path in UpdateCWD.
func TestStatusLineDisabledUpdateCWD(t *testing.T) {
	t.Parallel()
	sl := NewStatusLine(80)
	sl.enabled = false
	sl.UpdateCWD("/x")
	sl.UpdateExitCode(0)
	sl.UpdateModel("opus")
	assert.Empty(t, sl.Render())
}

// TestStatusLineRenderEmpty covers the "no parts" branch.
func TestStatusLineRenderEmpty(t *testing.T) {
	t.Parallel()
	sl := NewStatusLine(80)
	assert.Empty(t, sl.Render())
}

// --- hint_provider.go edge cases ---

// TestHintProviderEmptyKey covers the empty-input branch.
func TestHintProviderEmptyKey(t *testing.T) {
	t.Parallel()
	hp := NewHintProvider(func(_ context.Context, _ string) (<-chan TurnEvent, error) {
		return nil, nil
	})
	assert.Nil(t, hp.Hint(""))
	assert.Nil(t, hp.Hint("   "))
}

// TestHintProviderPendingSuppressesDuplicates covers the pending-deduplication branch.
func TestHintProviderPendingSuppressesDuplicates(t *testing.T) {
	t.Parallel()
	hp := NewHintProvider(func(_ context.Context, _ string) (<-chan TurnEvent, error) {
		return nil, nil
	})

	// Manually mark as pending.
	hp.mu.Lock()
	hp.pending["foo bar"] = true
	hp.mu.Unlock()

	assert.Nil(t, hp.Hint("foo bar"))
}

// TestHintProviderFetchAgentError covers the agent-error branch of fetchHints.
func TestHintProviderFetchAgentError(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	hp := NewHintProvider(func(_ context.Context, _ string) (<-chan TurnEvent, error) {
		defer close(done)
		return nil, errors.New("broken")
	})

	assert.Nil(t, hp.Hint("docker run --"))
	<-done
	// Allow the goroutine's cleanup to finish.
	hp.mu.Lock()
	pending := hp.pending["docker run --"]
	hp.mu.Unlock()
	assert.False(t, pending)
}

// --- script_generator.go edge cases ---

// TestScriptGeneratorNilAgent covers the "agent not available" branch.
func TestScriptGeneratorNilAgent(t *testing.T) {
	t.Parallel()
	workDir := "/project"
	sg := NewScriptGenerator(nil, nil, &workDir)
	_, err := sg.Generate(context.Background(), "do thing")
	assert.Error(t, err)
}

// TestScriptGeneratorNilWorkDir covers the nil-workDir branch.
func TestScriptGeneratorNilWorkDir(t *testing.T) {
	t.Parallel()
	var captured string
	agent := func(_ context.Context, msg string) (<-chan TurnEvent, error) {
		captured = msg
		ch := make(chan TurnEvent, 2)
		ch <- TurnEvent{Type: EventTextDelta, Text: "echo hi"}
		ch <- TurnEvent{Type: EventDone}
		close(ch)
		return ch, nil
	}
	sg := NewScriptGenerator(agent, nil, nil)
	_, err := sg.Generate(context.Background(), "do thing")
	assert.NoError(t, err)
	assert.Contains(t, captured, "do thing")
}

// TestScriptGeneratorAgentError covers the agentTurn error branch.
func TestScriptGeneratorAgentError(t *testing.T) {
	t.Parallel()
	wd := "/project"
	sg := NewScriptGenerator(func(_ context.Context, _ string) (<-chan TurnEvent, error) {
		return nil, errors.New("boom")
	}, nil, &wd)
	_, err := sg.Generate(context.Background(), "task")
	assert.Error(t, err)
}

// TestScriptGeneratorExecuteNilShellExec covers the nil-shellExec branch.
func TestScriptGeneratorExecuteNilShellExec(t *testing.T) {
	t.Parallel()
	wd := "/project"
	sg := NewScriptGenerator(nil, nil, &wd)
	_, _, exit, err := sg.Execute(context.Background(), "echo hi")
	assert.Error(t, err)
	assert.Equal(t, 1, exit)
}

// TestScriptGeneratorExecuteNilWorkDir covers the nil-workDir branch in Execute.
func TestScriptGeneratorExecuteNilWorkDir(t *testing.T) {
	t.Parallel()
	var gotWD string
	exec := func(_ context.Context, _ string, wd string) (string, string, int, error) {
		gotWD = wd
		return "", "", 0, nil
	}
	sg := NewScriptGenerator(nil, exec, nil)
	_, _, _, err := sg.Execute(context.Background(), "echo hi")
	assert.NoError(t, err)
	assert.Equal(t, "", gotWD)
}

// TestExtractScriptNoClosingFence covers the "no closing fence" branch.
func TestExtractScriptNoClosingFence(t *testing.T) {
	t.Parallel()
	script := extractScript("```bash\necho hi\n")
	assert.Equal(t, "echo hi", script)
}

// --- error_analyzer.go edge cases ---

// TestNewErrorAnalyzerNegativeMax defaults negative limits to 4096.
func TestNewErrorAnalyzerNegativeMax(t *testing.T) {
	t.Parallel()
	ea := NewErrorAnalyzer(func(_ context.Context, _ string) (<-chan TurnEvent, error) { return nil, nil }, -1)
	assert.Equal(t, 4096, ea.maxOutput)
}

// TestErrorAnalyzerBothEmpty covers the "no combined output" branch.
func TestErrorAnalyzerBothEmpty(t *testing.T) {
	t.Parallel()
	var prompt string
	ea := NewErrorAnalyzer(func(_ context.Context, msg string) (<-chan TurnEvent, error) {
		prompt = msg
		ch := make(chan TurnEvent, 1)
		close(ch)
		return ch, nil
	}, 4096)
	events, err := ea.Analyze(context.Background(), "cmd", "", "", 1)
	require.NoError(t, err)
	for range events {
	}
	// No output block rendered
	assert.NotContains(t, prompt, "```")
}

// --- line_reader.go edge cases ---

// TestSimpleLineReaderScannerError forces a scanner error via a failing reader.
func TestSimpleLineReaderScannerError(t *testing.T) {
	t.Parallel()

	r := NewSimpleLineReader(&erroringReader{err: errors.New("io boom")})
	_, err := r.ReadLine("> ")
	assert.Error(t, err)
}

// TestSimpleLineReaderHandlesPrompt ensures HandlesPrompt returns false.
func TestSimpleLineReaderHandlesPrompt(t *testing.T) {
	t.Parallel()
	r := NewSimpleLineReader(strings.NewReader(""))
	assert.False(t, r.HandlesPrompt())
}

// erroringReader returns a fixed error on every Read.
type erroringReader struct{ err error }

func (e *erroringReader) Read(_ []byte) (int, error) { return 0, e.err }

// Ensure the unused io import compiles cleanly.
var _ io.Reader = (*erroringReader)(nil)
