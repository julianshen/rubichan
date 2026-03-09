package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestApprovalPromptView(t *testing.T) {
	ap := NewApprovalPrompt("shell", `"rm -rf /tmp"`, 60)
	view := ap.View()
	assert.Contains(t, view, "shell")
	assert.Contains(t, view, "Ruby")
	assert.Contains(t, view, "(y)es")
}

func TestApprovalPromptResult(t *testing.T) {
	ap := NewApprovalPrompt("shell", `"ls"`, 60)
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
		ap := NewApprovalPrompt("shell", `"ls"`, 60)
		handled := ap.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)})
		assert.True(t, handled)
		assert.Equal(t, tt.result, ap.Result())
	}
}

func TestApprovalPromptHandleKeyUnknown(t *testing.T) {
	ap := NewApprovalPrompt("shell", `"ls"`, 60)
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
		ap := NewApprovalPrompt("shell", `"ls"`, 60)
		handled := ap.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)})
		assert.True(t, handled)
		assert.Equal(t, tt.result, ap.Result())
	}
}

func TestApprovalPromptViewContainsArgs(t *testing.T) {
	ap := NewApprovalPrompt("file", `"/etc/passwd"`, 80)
	view := ap.View()
	assert.Contains(t, view, "file")
	assert.Contains(t, view, "/etc/passwd")
	assert.Contains(t, view, "(n)o")
	assert.Contains(t, view, "(a)lways")
}

func TestApprovalPromptMinWidth(t *testing.T) {
	// Very small width should clamp to minimum
	ap := NewApprovalPrompt("shell", `"ls"`, 10)
	view := ap.View()
	assert.Contains(t, view, "shell")
	assert.Contains(t, view, "Ruby")
}

func TestApprovalResultConstants(t *testing.T) {
	// Verify all result values are distinct
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

func TestApprovalPromptDenyKey(t *testing.T) {
	ap := NewApprovalPrompt("shell", `"ls"`, 60)
	handled := ap.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	assert.True(t, handled)
	assert.Equal(t, ApprovalDenyAlways, ap.Result())
}

func TestApprovalPromptRiskLabel(t *testing.T) {
	ap := NewApprovalPrompt("shell", `"rm -rf /tmp"`, 60)
	view := ap.View()
	assert.Contains(t, view, "HIGH")
}

func TestApprovalPromptDestructiveWarning(t *testing.T) {
	ap := NewApprovalPrompt("shell", `"rm -rf /tmp"`, 60)
	view := ap.View()
	assert.Contains(t, view, "Destructive")
}

func TestApprovalPromptShowsDenyOption(t *testing.T) {
	ap := NewApprovalPrompt("shell", `"ls"`, 60)
	view := ap.View()
	assert.Contains(t, view, "(d)eny always")
}
