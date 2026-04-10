package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLinkifyFilePaths_AbsolutePath(t *testing.T) {
	result := LinkifyFilePaths("/foo/bar.go", "/home/user", true)
	assert.Contains(t, result, "\x1b]8;;file:///foo/bar.go\x1b\\")
	assert.Contains(t, result, "/foo/bar.go")
	assert.Contains(t, result, "\x1b]8;;\x1b\\")
}

func TestLinkifyFilePaths_RelativePath(t *testing.T) {
	result := LinkifyFilePaths("./src/main.go", "/home/user", true)
	assert.Contains(t, result, "file:///home/user/src/main.go")
}

func TestLinkifyFilePaths_NoMatch(t *testing.T) {
	input := "just some plain text with no file paths"
	assert.Equal(t, input, LinkifyFilePaths(input, "/home", true))
}

func TestLinkifyFilePaths_DisabledTerminal(t *testing.T) {
	input := "/foo/bar.go is a file"
	assert.Equal(t, input, LinkifyFilePaths(input, "/home", false))
}

func TestLinkifyFilePaths_MultiplePathsInLine(t *testing.T) {
	input := "modified: internal/tui/model.go and internal/tui/view.go"
	result := LinkifyFilePaths(input, "/proj", true)
	assert.Contains(t, result, "file:///proj/internal/tui/model.go")
	assert.Contains(t, result, "file:///proj/internal/tui/view.go")
}

func TestLinkifyFilePaths_PathLikeDir(t *testing.T) {
	result := LinkifyFilePaths("src/components/Button.tsx", "/proj", true)
	assert.Contains(t, result, "file:///proj/src/components/Button.tsx")
}

func TestLinkifyFilePaths_ProperURLFormat(t *testing.T) {
	// Verify proper file:// URL construction via url.URL.
	result := LinkifyFilePaths("/proj/src/main.go", "/home", true)
	assert.Contains(t, result, "file:///proj/src/main.go")
}

func TestLinkifyFilePaths_RejectsEscapeSequences(t *testing.T) {
	// A path containing an ESC character should not be linkified even when hyperlinks are enabled.
	input := "/foo/\x1b[31mevil\x1b[0m.go"
	assert.Equal(t, input, LinkifyFilePaths(input, "/home", true))
}
