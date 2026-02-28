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
	assert.Contains(t, view, "Allow")
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
	assert.Contains(t, view, "Allow")
}

func TestApprovalResultConstants(t *testing.T) {
	// Verify all result values are distinct
	results := []ApprovalResult{ApprovalPending, ApprovalYes, ApprovalNo, ApprovalAlways}
	seen := make(map[ApprovalResult]bool)
	for _, r := range results {
		assert.False(t, seen[r], "duplicate ApprovalResult value: %d", r)
		seen[r] = true
	}
}
