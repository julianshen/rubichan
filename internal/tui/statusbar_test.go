package tui

import (
	"testing"

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

func TestStatusBarDefaults(t *testing.T) {
	sb := NewStatusBar(80)
	result := sb.View()
	// Should render without panicking, even with zero values
	assert.Contains(t, result, "0/0")
	assert.Contains(t, result, "Turn 0/0")
	assert.Contains(t, result, "$0.00")
}
