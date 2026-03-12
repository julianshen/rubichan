package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestApprovalPromptView(t *testing.T) {
	ap := NewApprovalPrompt("shell", `"rm -rf /tmp"`, 60, nil)
	view := ap.View()
	// Should show display name, not raw tool name.
	assert.Contains(t, view, "Bash")
	assert.Contains(t, view, "[Y]")
}

func TestApprovalPromptResult(t *testing.T) {
	ap := NewApprovalPrompt("shell", `"ls"`, 60, nil)
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
		ap := NewApprovalPrompt("shell", `"ls"`, 60, nil)
		handled := ap.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)})
		assert.True(t, handled)
		assert.Equal(t, tt.result, ap.Result())
	}
}

func TestApprovalPromptHandleKeyUnknown(t *testing.T) {
	ap := NewApprovalPrompt("shell", `"ls"`, 60, nil)
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
		ap := NewApprovalPrompt("shell", `"ls"`, 60, nil)
		handled := ap.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)})
		assert.True(t, handled)
		assert.Equal(t, tt.result, ap.Result())
	}
}

func TestApprovalPromptViewContainsArgs(t *testing.T) {
	ap := NewApprovalPrompt("file_read", `"/etc/passwd"`, 80, nil)
	view := ap.View()
	assert.Contains(t, view, "Read file")
	assert.Contains(t, view, "/etc/passwd")
	assert.Contains(t, view, "[N]")
	assert.Contains(t, view, "[A]")
}

func TestApprovalPromptMinWidth(t *testing.T) {
	// Very small width should clamp to minimum.
	ap := NewApprovalPrompt("shell", `"ls"`, 10, nil)
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
	ap := NewApprovalPrompt("shell", `"rm -rf /tmp"`, 60, nil)
	view := ap.View()
	// High risk icon should appear.
	assert.Contains(t, view, "⚠")
}

func TestApprovalPromptDestructiveWarning(t *testing.T) {
	ap := NewApprovalPrompt("shell", `"rm -rf /tmp"`, 60, nil)
	view := ap.View()
	assert.Contains(t, view, "Destructive")
}

func TestApprovalPromptMediumRiskLabel(t *testing.T) {
	ap := NewApprovalPrompt("file_write", `"/tmp/foo.txt"`, 60, nil)
	view := ap.View()
	assert.Contains(t, view, "●")
	assert.Contains(t, view, "Write file")
}

func TestApprovalPromptLowRiskLabel(t *testing.T) {
	ap := NewApprovalPrompt("file_read", `"/tmp/foo.txt"`, 60, nil)
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
	ap := NewApprovalPrompt("\x1b[31mfake_safe\x1b[0m", `"\x1b[32msafe_args\x1b[0m"`, 80, nil)
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

// --- Configurable options tests ---

func TestOptionsForRisk_DestructiveOnlyYesNo(t *testing.T) {
	opts := OptionsForRisk("shell", `"rm -rf /"`)
	assert.Equal(t, []ApprovalResult{ApprovalYes, ApprovalNo}, opts)
}

func TestOptionsForRisk_HighRiskNoDestructive(t *testing.T) {
	opts := OptionsForRisk("shell", `"ls -la"`)
	assert.Equal(t, []ApprovalResult{ApprovalYes, ApprovalNo, ApprovalAlways}, opts)
}

func TestOptionsForRisk_LowRiskAllOptions(t *testing.T) {
	opts := OptionsForRisk("file_read", `"/tmp/foo"`)
	assert.Equal(t, []ApprovalResult{ApprovalYes, ApprovalNo, ApprovalAlways, ApprovalDenyAlways}, opts)
}

func TestApprovalPromptDestructiveHidesAlways(t *testing.T) {
	// Destructive commands should not show "always" or "deny always" options.
	ap := NewApprovalPrompt("shell", `"rm -rf /tmp"`, 60, nil)
	view := ap.View()
	assert.Contains(t, view, "[Y]")
	assert.Contains(t, view, "[N]")
	assert.NotContains(t, view, "[A]")
	assert.NotContains(t, view, "[D]")
}

func TestApprovalPromptDestructiveRejectsAlwaysKey(t *testing.T) {
	// Pressing 'a' on a destructive command should be ignored.
	ap := NewApprovalPrompt("shell", `"rm -rf /tmp"`, 60, nil)
	handled := ap.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	assert.False(t, handled)
	assert.False(t, ap.Done())
}

func TestApprovalPromptCustomOptions(t *testing.T) {
	// Caller can specify custom options.
	opts := []ApprovalResult{ApprovalYes, ApprovalNo}
	ap := NewApprovalPrompt("file_read", `"/etc/passwd"`, 60, opts)
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
	ap := NewApprovalPrompt("file_read", `"ls"`, 60, nil)
	view := ap.View()
	assert.Contains(t, view, "[D]")
}

func TestApprovalPromptDenyKey(t *testing.T) {
	// Low-risk tool allows deny always.
	ap := NewApprovalPrompt("file_read", `"ls"`, 60, nil)
	handled := ap.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	assert.True(t, handled)
	assert.Equal(t, ApprovalDenyAlways, ap.Result())
}

func TestApprovalPromptHighRiskHidesDenyAlways(t *testing.T) {
	// High-risk (non-destructive) should not show deny-always.
	ap := NewApprovalPrompt("shell", `"ls"`, 60, nil)
	view := ap.View()
	assert.NotContains(t, view, "[D]")

	handled := ap.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	assert.False(t, handled)
}
