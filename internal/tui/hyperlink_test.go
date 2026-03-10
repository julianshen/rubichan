package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSupportsHyperlinks_iTerm(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	assert.True(t, SupportsHyperlinks())
}

func TestSupportsHyperlinks_WezTerm(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "WezTerm")
	assert.True(t, SupportsHyperlinks())
}

func TestSupportsHyperlinks_Unknown(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "unknown-terminal")
	assert.False(t, SupportsHyperlinks())
}

func TestSupportsHyperlinks_Unset(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "")
	assert.False(t, SupportsHyperlinks())
}

func TestLinkifyFilePaths_AbsolutePath(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	result := LinkifyFilePaths("/foo/bar.go", "/home/user")
	assert.Contains(t, result, "\x1b]8;;file:///foo/bar.go\x1b\\")
	assert.Contains(t, result, "/foo/bar.go")
	assert.Contains(t, result, "\x1b]8;;\x1b\\")
}

func TestLinkifyFilePaths_RelativePath(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	result := LinkifyFilePaths("./src/main.go", "/home/user")
	assert.Contains(t, result, "file:///home/user/src/main.go")
}

func TestLinkifyFilePaths_NoMatch(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	input := "just some plain text with no file paths"
	assert.Equal(t, input, LinkifyFilePaths(input, "/home"))
}

func TestLinkifyFilePaths_DisabledTerminal(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "unknown")
	input := "/foo/bar.go is a file"
	assert.Equal(t, input, LinkifyFilePaths(input, "/home"))
}

func TestLinkifyFilePaths_MultiplePathsInLine(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	input := "modified: internal/tui/model.go and internal/tui/view.go"
	result := LinkifyFilePaths(input, "/proj")
	assert.Contains(t, result, "file:///proj/internal/tui/model.go")
	assert.Contains(t, result, "file:///proj/internal/tui/view.go")
}

func TestLinkifyFilePaths_PathLikeDir(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	result := LinkifyFilePaths("src/components/Button.tsx", "/proj")
	assert.Contains(t, result, "file:///proj/src/components/Button.tsx")
}

func TestLinkifyFilePaths_ProperURLFormat(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	// Verify proper file:// URL construction via url.URL.
	result := LinkifyFilePaths("/proj/src/main.go", "/home")
	assert.Contains(t, result, "file:///proj/src/main.go")
}

func TestLinkifyFilePaths_RejectsEscapeSequences(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	// A path containing an ESC character should not be linkified.
	input := "/foo/\x1b[31mevil\x1b[0m.go"
	assert.Equal(t, input, LinkifyFilePaths(input, "/home"))
}
