package tui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestStatusBarRender(t *testing.T) {
	sb := NewStatusBar(80)
	sb.SetModel("claude-sonnet-4-5")
	sb.SetTokens(1200, 100000)
	sb.SetTurn(3, 50)
	sb.SetCost(0.02)
	result := sb.View()
	assert.Contains(t, result, "claude-sonnet-4-5")
	assert.Contains(t, result, "1.2k/100k")
	assert.Contains(t, result, "Turn 3/50")
	assert.Contains(t, result, "$0.02")
}

func TestStatusBarTokenFormat(t *testing.T) {
	sb := NewStatusBar(80)
	sb.SetTokens(500, 8000)
	result := sb.View()
	assert.Contains(t, result, "500/8k")
}

func TestFormatTokens(t *testing.T) {
	assert.Equal(t, "0", formatTokens(0))
	assert.Equal(t, "999", formatTokens(999))
	assert.Equal(t, "1k", formatTokens(1000))
	assert.Equal(t, "1.5k", formatTokens(1500))
	assert.Equal(t, "100k", formatTokens(100000))
	assert.Equal(t, "1000k", formatTokens(1000000))
}

func TestStatusBarContainsPersona(t *testing.T) {
	sb := NewStatusBar(80)
	sb.SetModel("claude-sonnet-4-5")
	result := sb.View()
	assert.Contains(t, result, "Ruby")
}

func TestStatusBarDefaults(t *testing.T) {
	sb := NewStatusBar(80)
	result := sb.View()
	// Should render without panicking, even with zero values
	assert.Contains(t, result, "0/0")
	assert.Contains(t, result, "Turn 0/0")
	assert.Contains(t, result, "$0.00")
}

// --- Elapsed time ---

func TestStatusBarElapsedTime(t *testing.T) {
	sb := NewStatusBar(80)
	sb.SetModel("test-model")
	sb.SetElapsed(4100 * time.Millisecond)
	result := sb.View()
	assert.Contains(t, result, "4.1s")
}

func TestStatusBarElapsedTimeRoundsDown(t *testing.T) {
	sb := NewStatusBar(80)
	sb.SetElapsed(500 * time.Millisecond)
	result := sb.View()
	assert.Contains(t, result, "0.5s")
}

func TestStatusBarElapsedTimeLarge(t *testing.T) {
	sb := NewStatusBar(80)
	sb.SetElapsed(65 * time.Second)
	result := sb.View()
	assert.Contains(t, result, "1m5s")
}

func TestStatusBarNoElapsedWhenZero(t *testing.T) {
	sb := NewStatusBar(80)
	sb.SetModel("test-model")
	result := sb.View()
	assert.NotContains(t, result, "⏱")
}

func TestStatusBarClearElapsed(t *testing.T) {
	sb := NewStatusBar(80)
	sb.SetElapsed(3 * time.Second)
	sb.ClearElapsed()
	result := sb.View()
	assert.NotContains(t, result, "⏱")
}

// --- Git branch ---

func TestStatusBarGitBranch(t *testing.T) {
	sb := NewStatusBar(80)
	sb.SetModel("test-model")
	sb.SetGitBranch("feature/cool-stuff")
	result := sb.View()
	assert.Contains(t, result, "feature/cool-stuff")
}

func TestStatusBarNoGitBranchWhenEmpty(t *testing.T) {
	sb := NewStatusBar(80)
	sb.SetModel("test-model")
	result := sb.View()
	assert.NotContains(t, result, "⎇")
}

func TestFormatElapsed(t *testing.T) {
	assert.Equal(t, "0.5s", formatElapsed(500*time.Millisecond))
	assert.Equal(t, "3.0s", formatElapsed(3*time.Second))
	assert.Equal(t, "4.1s", formatElapsed(4100*time.Millisecond))
	assert.Equal(t, "1m5s", formatElapsed(65*time.Second))
	assert.Equal(t, "2m30s", formatElapsed(150*time.Second))
}

// --- Subagent status bar tests ---

func TestStatusBarSetSubagent_ShowsNameInView(t *testing.T) {
	t.Parallel()
	sb := NewStatusBar(80)
	sb.SetModel("test-model")
	sb.SetSubagent("code-review")
	result := sb.View()
	assert.Contains(t, result, "code-review", "subagent name should appear in status bar view")
}

func TestStatusBarSetSubagent_EmptyClearsFromView(t *testing.T) {
	t.Parallel()
	sb := NewStatusBar(80)
	sb.SetModel("test-model")
	sb.SetSubagent("code-review")
	sb.SetSubagent("")
	result := sb.View()
	assert.NotContains(t, result, "code-review", "clearing subagent should remove it from view")
	assert.NotContains(t, result, "🔄", "clearing subagent should remove the icon from view")
}

func TestStatusBarAllFields(t *testing.T) {
	sb := NewStatusBar(80)
	sb.SetModel("claude-sonnet-4-5")
	sb.SetTokens(1200, 100000)
	sb.SetTurn(3, 50)
	sb.SetCost(0.02)
	sb.SetGitBranch("main")
	sb.SetElapsed(2500 * time.Millisecond)
	result := sb.View()
	assert.Contains(t, result, "claude-sonnet-4-5")
	assert.Contains(t, result, "1.2k/100k")
	assert.Contains(t, result, "Turn 3/50")
	assert.Contains(t, result, "$0.02")
	assert.Contains(t, result, "main")
	assert.Contains(t, result, "2.5s")
}
