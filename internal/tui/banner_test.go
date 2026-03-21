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
		trimmed := strings.TrimSpace(line)
		snippet := trimmed
		if len(snippet) > 10 {
			snippet = snippet[:10]
		}
		assert.Contains(t, rendered, snippet,
			"rendered banner should contain text from each line")
	}
}

func TestRenderBannerPreservesLineCount(t *testing.T) {
	bannerLines := strings.Split(Banner, "\n")
	renderedLines := strings.Split(RenderBanner(), "\n")
	// +1 for the welcome subtitle line appended by RenderBanner
	assert.Equal(t, len(bannerLines)+1, len(renderedLines),
		"rendered banner should have original lines plus welcome subtitle")
}

func TestRenderBannerContainsWelcome(t *testing.T) {
	rendered := RenderBanner()
	assert.Contains(t, rendered, "Ruby")
}

// --- Compact banner tests ---

func TestRenderCompactBanner_ContainsRubichan(t *testing.T) {
	t.Parallel()
	result := RenderCompactBanner()
	assert.Contains(t, result, "rubichan", "compact banner should contain 'rubichan'")
}

func TestRenderCompactBanner_ContainsPersonaStatusPrefix(t *testing.T) {
	t.Parallel()
	result := RenderCompactBanner()
	// The persona status prefix (e.g. "Ruby") should appear in the compact banner.
	assert.Contains(t, result, "Ruby", "compact banner should contain the persona status prefix")
}

func TestNewModelInitialContent(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)

	content := m.content.String()
	assert.Contains(t, content, ".-')", "initial viewport should contain the banner ASCII art")
}
