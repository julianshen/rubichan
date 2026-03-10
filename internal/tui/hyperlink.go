package tui

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// supportedTerminals lists TERM_PROGRAM values known to support OSC 8.
var supportedTerminals = map[string]bool{
	"iTerm.app":      true,
	"WezTerm":        true,
	"kitty":          true,
	"ghostty":        true,
	"vscode":         true,
	"Hyper":          true,
	"Tabby":          true,
	"rio":            true,
	"contour":        true,
	"alacritty":      true,
	"Apple_Terminal": true,
}

// SupportsHyperlinks returns true if the current terminal supports OSC 8 links.
func SupportsHyperlinks() bool {
	tp := os.Getenv("TERM_PROGRAM")
	return supportedTerminals[tp]
}

// filePathPattern matches file paths that look like:
// - Absolute paths: /foo/bar.go
// - Relative paths: ./foo/bar.go, ../foo/bar.go
// - Directory paths: dir/subdir/file.ext (must contain / and end with extension)
var filePathPattern = regexp.MustCompile(`(?:^|[\s:])(/[^\s:]+\.[a-zA-Z0-9]+|\.\.?/[^\s:]+\.[a-zA-Z0-9]+|[a-zA-Z0-9_][a-zA-Z0-9_./+-]*\.[a-zA-Z0-9]+)`)

// LinkifyFilePaths wraps recognized file paths in OSC 8 hyperlinks.
// Only activates when the terminal supports it.
// TODO: Wire into viewportContent() once Model carries a workDir field.
func LinkifyFilePaths(text string, workDir string) string {
	if !SupportsHyperlinks() {
		return text
	}

	return filePathPattern.ReplaceAllStringFunc(text, func(match string) string {
		// Preserve leading whitespace/colon
		prefix := ""
		path := match
		if len(path) > 0 && (path[0] == ' ' || path[0] == '\t' || path[0] == ':') {
			prefix = string(path[0])
			path = path[1:]
		}

		// Only linkify paths that contain a slash (to avoid matching random words.ext)
		if !strings.Contains(path, "/") {
			return match
		}

		absPath := path
		if !filepath.IsAbs(path) {
			cleaned := strings.TrimPrefix(path, "./")
			absPath = filepath.Join(workDir, cleaned)
		}

		// Reject paths containing control characters to prevent terminal injection.
		if strings.ContainsAny(absPath, "\x1b\x00\x07") {
			return match
		}

		fileURL := (&url.URL{Scheme: "file", Path: absPath}).String()
		link := "\x1b]8;;" + fileURL + "\x1b\\" + path + "\x1b]8;;\x1b\\"
		return prefix + link
	})
}
