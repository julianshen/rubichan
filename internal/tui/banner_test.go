package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBannerNotEmpty(t *testing.T) {
	assert.NotEmpty(t, Banner, "Banner constant should contain ASCII art")
}

func TestRenderBannerContainsAllLines(t *testing.T) {
	rendered := RenderBanner()

	// The rendered output should contain text from every line of the banner.
	lines := strings.Split(Banner, "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		// Each line's text content should appear in the rendered output
		// (lipgloss adds ANSI codes around it but preserves the text).
		assert.Contains(t, rendered, strings.TrimSpace(line)[:10],
			"rendered banner should contain text from each line")
	}
}

func TestRenderBannerPreservesLineCount(t *testing.T) {
	bannerLines := strings.Split(Banner, "\n")
	renderedLines := strings.Split(RenderBanner(), "\n")
	assert.Equal(t, len(bannerLines), len(renderedLines),
		"rendered banner should have same number of lines as original")
}

func TestNewModelInitialContent(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)

	content := m.content.String()
	assert.Contains(t, content, ".-')", "initial viewport should contain the banner ASCII art")
}
