package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBannerNotEmpty(t *testing.T) {
	assert.NotEmpty(t, Banner, "Banner constant should contain ASCII art")
}

func TestNewModelInitialContent(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil)

	content := m.content.String()
	assert.Contains(t, content, ".-')", "initial viewport should contain the banner ASCII art")
}
