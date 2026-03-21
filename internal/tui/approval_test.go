package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestApprovalPromptView(t *testing.T) {
	ap := NewApprovalPrompt("shell", `{"command":"rm -rf /tmp"}`, "", 60, nil)
	view := ap.View()
	// Should show display name, not raw tool name.
	assert.Contains(t, view, "Bash")
	// Should show the command, not raw JSON.
	assert.Contains(t, view, "rm -rf /tmp")
	assert.NotContains(t, view, `"command"`)
	assert.Contains(t, view, "[Y]")
}

func TestApprovalPromptResult(t *testing.T) {
	ap := NewApprovalPrompt("shell", `{"command":"ls"}`, "", 60, nil)
	assert.False(t, ap.Done())

	ap.SetResult(ApprovalYes)
	assert.True(t, ap.Done())
	assert.Equal(t, ApprovalYes, ap.Result())
}

func TestApprovalPromptHandleKey(t *testing.T) {
	tests := []struct {
		key    string
		result ApprovalResult
	}{
		{"y", ApprovalYes},
		{"n", ApprovalNo},
		{"a", ApprovalAlways},
	}
	for _, tt := range tests {
		ap := NewApprovalPrompt("shell", `{"command":"ls"}`, "", 60, nil)
		handled := ap.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)})
		assert.True(t, handled)
		assert.Equal(t, tt.result, ap.Result())
	}
}

func TestApprovalPromptHandleKeyUnknown(t *testing.T) {
	ap := NewApprovalPrompt("shell", `{"command":"ls"}`, "", 60, nil)
	handled := ap.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	assert.False(t, handled)
	assert.False(t, ap.Done())
}

func TestApprovalPromptHandleKeyUppercase(t *testing.T) {
	tests := []struct {
		key    string
		result ApprovalResult
	}{
		{"Y", ApprovalYes},
		{"N", ApprovalNo},
		{"A", ApprovalAlways},
	}
	for _, tt := range tests {
		ap := NewApprovalPrompt("shell", `{"command":"ls"}`, "", 60, nil)
		handled := ap.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)})
		assert.True(t, handled)
		assert.Equal(t, tt.result, ap.Result())
	}
}

func TestApprovalPromptViewContainsArgs(t *testing.T) {
	ap := NewApprovalPrompt("file", `{"operation":"read","path":"/etc/passwd"}`, "", 80, nil)
	view := ap.View()
	assert.Contains(t, view, "/etc/passwd")
	assert.NotContains(t, view, `"operation"`)
	assert.Contains(t, view, "[N]")
	assert.Contains(t, view, "[A]")
}

func TestApprovalPromptMinWidth(t *testing.T) {
	// Very small width should clamp to minimum.
	ap := NewApprovalPrompt("shell", `{"command":"ls"}`, "", 10, nil)
	view := ap.View()
	assert.Contains(t, view, "Bash")
}

func TestApprovalResultConstants(t *testing.T) {
	// Verify all result values are distinct.
	results := []ApprovalResult{ApprovalPending, ApprovalYes, ApprovalNo, ApprovalAlways, ApprovalDenyAlways}
	seen := make(map[ApprovalResult]bool)
	for _, r := range results {
		assert.False(t, seen[r], "duplicate ApprovalResult value: %d", r)
		seen[r] = true
	}
}

func TestRiskLevel_ShellIsHigh(t *testing.T) {
	assert.Equal(t, RiskHigh, classifyRisk("shell"))
	assert.Equal(t, RiskHigh, classifyRisk("bash"))
}

func TestRiskLevel_FileReadIsLow(t *testing.T) {
	assert.Equal(t, RiskLow, classifyRisk("file_read"))
	assert.Equal(t, RiskLow, classifyRisk("read_file"))
	assert.Equal(t, RiskLow, classifyRisk("search"))
}

func TestRiskLevel_FileWriteIsMedium(t *testing.T) {
	assert.Equal(t, RiskMedium, classifyRisk("file_write"))
	assert.Equal(t, RiskMedium, classifyRisk("write_file"))
	assert.Equal(t, RiskMedium, classifyRisk("patch"))
}

func TestIsDestructiveCommand(t *testing.T) {
	assert.True(t, isDestructiveCommand(`"rm -rf /"`))
	assert.True(t, isDestructiveCommand(`"git reset --hard"`))
	assert.True(t, isDestructiveCommand(`"git push --force"`))
	assert.True(t, isDestructiveCommand(`"DROP TABLE users"`))
	assert.False(t, isDestructiveCommand(`"ls -la"`))
	assert.False(t, isDestructiveCommand(`"git status"`))
}

func TestApprovalPromptRiskLabel(t *testing.T) {
	ap := NewApprovalPrompt("shell", `{"command":"rm -rf /tmp"}`, "", 60, nil)
	view := ap.View()
	// High risk icon should appear.
	assert.Contains(t, view, "⚠")
}

func TestApprovalPromptDestructiveWarning(t *testing.T) {
	ap := NewApprovalPrompt("shell", `{"command":"rm -rf /tmp"}`, "", 60, nil)
	view := ap.View()
	assert.Contains(t, view, "Destructive")
}

func TestApprovalPromptMediumRiskLabel(t *testing.T) {
	ap := NewApprovalPrompt("file_write", `{"operation":"write","path":"/tmp/foo.txt"}`, "", 60, nil)
	view := ap.View()
	assert.Contains(t, view, "●")
	assert.Contains(t, view, "Write file")
}

func TestApprovalPromptLowRiskLabel(t *testing.T) {
	ap := NewApprovalPrompt("file_read", `{"operation":"read","path":"/tmp/foo.txt"}`, "", 60, nil)
	view := ap.View()
	assert.Contains(t, view, "●")
	assert.Contains(t, view, "Read file")
}

func TestStripANSI(t *testing.T) {
	assert.Equal(t, "hello", stripANSI("hello"))
	assert.Equal(t, "evil", stripANSI("\x1b[31mevil\x1b[0m"))
	assert.Equal(t, "rm -rf /", stripANSI("rm -rf /"))
	// OSC 8 hyperlink sequence (ST-terminated)
	assert.Equal(t, "click", stripANSI("\x1b]8;;http://evil.com\x1b\\click\x1b]8;;\x1b\\"))
	// OSC sequence (BEL-terminated)
	assert.Equal(t, "", stripANSI("\x1b]0;title\x07"))
}

func TestApprovalPromptSanitizesANSI(t *testing.T) {
	// Tool name with ANSI injection should be stripped in the rendered view.
	ap := NewApprovalPrompt("\x1b[31mfake_safe\x1b[0m", `"\x1b[32msafe_args\x1b[0m"`, "", 80, nil)
	view := ap.View()
	assert.NotContains(t, view, "\x1b[31m")
	assert.NotContains(t, view, "\x1b[32m")
}

func TestToolDisplayName(t *testing.T) {
	assert.Equal(t, "Bash", toolDisplayName("shell"))
	assert.Equal(t, "Bash", toolDisplayName("bash"))
	assert.Equal(t, "Read file", toolDisplayName("file_read"))
	assert.Equal(t, "Write file", toolDisplayName("file_write"))
	assert.Equal(t, "Edit file", toolDisplayName("edit"))
	assert.Equal(t, "Search", toolDisplayName("search"))
	assert.Equal(t, "custom_tool", toolDisplayName("custom_tool"))
}

// --- formatToolArgs tests ---

func TestFormatToolArgs_ShellCommand(t *testing.T) {
	result := formatToolArgs("shell", `{"command":"ls -la /tmp"}`)
	assert.Equal(t, "ls -la /tmp", result)
}

func TestFormatToolArgs_ShellWithDescription(t *testing.T) {
	result := formatToolArgs("shell", `{"command":"npm install","description":"Install dependencies"}`)
	assert.Contains(t, result, "Install dependencies")
	assert.Contains(t, result, "npm install")
}

func TestFormatToolArgs_FileRead(t *testing.T) {
	result := formatToolArgs("file", `{"operation":"read","path":"internal/tui/model.go"}`)
	assert.Equal(t, "internal/tui/model.go", result)
}

func TestFormatToolArgs_FileWrite(t *testing.T) {
	result := formatToolArgs("file", `{"operation":"write","path":"foo.go","content":"package main"}`)
	assert.Equal(t, "foo.go", result)
}

func TestFormatToolArgs_FilePatch(t *testing.T) {
	result := formatToolArgs("file", `{"operation":"patch","path":"foo.go","old_string":"func old()","new_string":"func new()"}`)
	assert.Contains(t, result, "foo.go")
	assert.Contains(t, result, "func old()")
}

func TestFormatToolArgs_FilePatchLongOldString(t *testing.T) {
	long := "this is a very long string that should be truncated because it exceeds sixty characters total"
	result := formatToolArgs("file", `{"operation":"patch","path":"foo.go","old_string":"`+long+`","new_string":"short"}`)
	assert.Contains(t, result, "foo.go")
	assert.Contains(t, result, "...")
	assert.NotContains(t, result, long) // should be truncated
}

func TestFormatToolArgs_Search(t *testing.T) {
	result := formatToolArgs("search", `{"pattern":"TODO","path":"internal/"}`)
	assert.Equal(t, "TODO in internal/", result)
}

func TestFormatToolArgs_SearchNoPath(t *testing.T) {
	result := formatToolArgs("search", `{"pattern":"TODO"}`)
	assert.Equal(t, "TODO", result)
}

func TestFormatToolArgs_Grep(t *testing.T) {
	result := formatToolArgs("grep", `{"pattern":"func main","path":"cmd/"}`)
	assert.Equal(t, "func main in cmd/", result)
}

func TestFormatToolArgs_Glob(t *testing.T) {
	result := formatToolArgs("glob", `{"pattern":"**/*.go","path":"internal/"}`)
	assert.Equal(t, "**/*.go in internal/", result)
}

func TestFormatToolArgs_Edit(t *testing.T) {
	result := formatToolArgs("edit", `{"file_path":"main.go","old_string":"old","new_string":"new"}`)
	assert.Contains(t, result, "main.go")
	assert.Contains(t, result, "old")
}

func TestFormatToolArgs_HTTP(t *testing.T) {
	result := formatToolArgs("http", `{"url":"https://example.com/api"}`)
	assert.Equal(t, "https://example.com/api", result)
}

func TestFormatToolArgs_RawString(t *testing.T) {
	// Non-JSON input should be returned as-is (trimmed of quotes).
	result := formatToolArgs("shell", `"ls -la"`)
	assert.Equal(t, "ls -la", result)
}

func TestFormatToolArgs_UnknownToolFallback(t *testing.T) {
	result := formatToolArgs("custom_tool", `{"query":"find stuff"}`)
	assert.Equal(t, "find stuff", result)
}

func TestFormatToolArgs_EmptyArgs(t *testing.T) {
	result := formatToolArgs("shell", `{}`)
	assert.Equal(t, "(no arguments)", result)
}

// --- Configurable options tests ---

func TestOptionsForRisk_DestructiveOnlyYesNo(t *testing.T) {
	opts := OptionsForRisk("shell", `{"command":"rm -rf /"}`)
	assert.Equal(t, []ApprovalResult{ApprovalYes, ApprovalNo}, opts)
}

func TestOptionsForRisk_HighRiskNoDestructive(t *testing.T) {
	opts := OptionsForRisk("shell", `{"command":"ls -la"}`)
	assert.Equal(t, []ApprovalResult{ApprovalYes, ApprovalNo, ApprovalAlways}, opts)
}

func TestOptionsForRisk_LowRiskAllOptions(t *testing.T) {
	opts := OptionsForRisk("file_read", `{"path":"/tmp/foo"}`)
	assert.Equal(t, []ApprovalResult{ApprovalYes, ApprovalNo, ApprovalAlways, ApprovalDenyAlways}, opts)
}

func TestApprovalPromptDestructiveHidesAlways(t *testing.T) {
	// Destructive commands should not show "always" or "deny always" options.
	ap := NewApprovalPrompt("shell", `{"command":"rm -rf /tmp"}`, "", 60, nil)
	view := ap.View()
	assert.Contains(t, view, "[Y]")
	assert.Contains(t, view, "[N]")
	assert.NotContains(t, view, "[A]")
	assert.NotContains(t, view, "[D]")
}

func TestApprovalPromptDestructiveRejectsAlwaysKey(t *testing.T) {
	// Pressing 'a' on a destructive command should be ignored.
	ap := NewApprovalPrompt("shell", `{"command":"rm -rf /tmp"}`, "", 60, nil)
	handled := ap.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	assert.False(t, handled)
	assert.False(t, ap.Done())
}

func TestApprovalPromptCustomOptions(t *testing.T) {
	// Caller can specify custom options.
	opts := []ApprovalResult{ApprovalYes, ApprovalNo}
	ap := NewApprovalPrompt("file_read", `{"path":"/etc/passwd"}`, "", 60, opts)
	view := ap.View()
	assert.Contains(t, view, "[Y]")
	assert.Contains(t, view, "[N]")
	assert.NotContains(t, view, "[A]")
	assert.NotContains(t, view, "[D]")

	// Key 'a' should be rejected since AlwaysApprove is not in options.
	handled := ap.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	assert.False(t, handled)
}

func TestApprovalPromptShowsDenyOption(t *testing.T) {
	// Low-risk tool should show deny always option.
	ap := NewApprovalPrompt("file_read", `{"path":"foo.go"}`, "", 60, nil)
	view := ap.View()
	assert.Contains(t, view, "[D]")
}

func TestApprovalPromptDenyKey(t *testing.T) {
	// Low-risk tool allows deny always.
	ap := NewApprovalPrompt("file_read", `{"path":"foo.go"}`, "", 60, nil)
	handled := ap.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	assert.True(t, handled)
	assert.Equal(t, ApprovalDenyAlways, ap.Result())
}

func TestApprovalPromptHighRiskHidesDenyAlways(t *testing.T) {
	// High-risk (non-destructive) should not show deny-always.
	ap := NewApprovalPrompt("shell", `{"command":"ls"}`, "", 60, nil)
	view := ap.View()
	assert.NotContains(t, view, "[D]")

	handled := ap.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	assert.False(t, handled)
}

// --- View shows formatted args, not raw JSON ---

func TestApprovalPromptView_NoRawJSON(t *testing.T) {
	ap := NewApprovalPrompt("shell", `{"command":"git status","description":"Check working tree"}`, "", 80, nil)
	view := ap.View()
	// Should show the description and command, not JSON keys.
	assert.Contains(t, view, "Check working tree")
	assert.Contains(t, view, "git status")
	assert.NotContains(t, view, `"command"`)
	assert.NotContains(t, view, `"description"`)
}

func TestApprovalPromptView_FileShowsPath(t *testing.T) {
	ap := NewApprovalPrompt("file", `{"operation":"read","path":"src/main.go"}`, "", 80, nil)
	view := ap.View()
	assert.Contains(t, view, "src/main.go")
	assert.NotContains(t, view, `"operation"`)
}

func TestApprovalPromptView_SearchShowsPattern(t *testing.T) {
	ap := NewApprovalPrompt("search", `{"pattern":"func Test","path":"internal/"}`, "", 80, nil)
	view := ap.View()
	assert.Contains(t, view, "func Test")
	assert.Contains(t, view, "internal/")
	assert.NotContains(t, view, `"pattern"`)
}

// --- workDir in approval tests ---

func TestApprovalPromptView_WorkDirShownForHighRisk(t *testing.T) {
	t.Parallel()
	ap := NewApprovalPrompt("shell", `{"command":"ls"}`, "/home/user/project", 80, nil)
	view := ap.View()
	assert.Contains(t, view, "cwd:", "high-risk tool with non-empty workDir should show cwd")
	assert.Contains(t, view, "/home/user/project")
}

func TestApprovalPromptView_WorkDirHiddenForLowRisk(t *testing.T) {
	t.Parallel()
	ap := NewApprovalPrompt("file_read", `{"path":"foo.go"}`, "/home/user/project", 80, nil)
	view := ap.View()
	assert.NotContains(t, view, "cwd:", "low-risk tool should NOT show cwd even with non-empty workDir")
}

func TestApprovalPromptView_EmptyWorkDirNotShown(t *testing.T) {
	t.Parallel()
	ap := NewApprovalPrompt("shell", `{"command":"ls"}`, "", 80, nil)
	view := ap.View()
	assert.NotContains(t, view, "cwd:", "empty workDir should NOT show cwd even for high-risk tool")
}
