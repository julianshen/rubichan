package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
)

// TestAdaptiveColors verifies that the primary theme colors are
// lipgloss.AdaptiveColor, ensuring proper light/dark terminal support.
// This is a compile-time guarantee backed by a runtime type assertion.
func TestAdaptiveColors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		color lipgloss.TerminalColor
	}{
		{"colorPrimary", colorPrimary},
		{"colorPrimaryBold", colorPrimaryBold},
		{"colorPrimaryLight", colorPrimaryLight},
		{"colorPrimaryDim", colorPrimaryDim},
		{"colorAccent", colorAccent},
		{"colorAccentDim", colorAccentDim},
		{"colorAccentGlow", colorAccentGlow},
		{"colorSuccess", colorSuccess},
		{"colorWarning", colorWarning},
		{"colorDanger", colorDanger},
		{"colorInfo", colorInfo},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, ok := tt.color.(lipgloss.AdaptiveColor)
			assert.True(t, ok, "%s should be lipgloss.AdaptiveColor", tt.name)
		})
	}
}
