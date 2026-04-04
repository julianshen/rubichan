package shell

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Event type constants for TurnEvent.Type.
const (
	EventTextDelta  = "text_delta"
	EventToolCall   = "tool_call"
	EventToolResult = "tool_result"
	EventDone       = "done"
	EventError      = "error"
)

// Status line segment key constants.
const (
	SegmentCWD      = "cwd"
	SegmentBranch   = "branch"
	SegmentExitCode = "exitcode"
	SegmentModel    = "model"
)

// shortenHome replaces a leading homeDir prefix with ~.
func shortenHome(path, homeDir string) string {
	if homeDir == "" || !strings.HasPrefix(path, homeDir) {
		return path
	}
	rest := path[len(homeDir):]
	if rest == "" {
		return "~"
	}
	if rest[0] == '/' {
		return "~" + rest
	}
	return path
}

// collectTurnText drains a TurnEvent channel and returns the concatenated text_delta content.
func collectTurnText(events <-chan TurnEvent) string {
	var b strings.Builder
	for event := range events {
		if event.Type == EventTextDelta {
			b.WriteString(event.Text)
		}
	}
	return b.String()
}

// writeOutput writes s to w, appending a newline if s doesn't already end with one.
func writeOutput(w io.Writer, s string) {
	if s == "" {
		return
	}
	fmt.Fprint(w, s)
	if !strings.HasSuffix(s, "\n") {
		fmt.Fprintln(w)
	}
}

// truncateWithNotice truncates s to maxLen runes, appending a notice if truncated.
func truncateWithNotice(s string, maxLen int) string {
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	truncated := truncateRunes(s, maxLen)
	remaining := utf8.RuneCountInString(s) - maxLen
	return truncated + fmt.Sprintf("\n... (truncated, %d more chars)", remaining)
}

// truncateRunes returns the first n runes of s.
func truncateRunes(s string, n int) string {
	count := 0
	for i := range s {
		if count >= n {
			return s[:i]
		}
		count++
	}
	return s
}

// safePackageName matches only characters safe for use in package manager commands.
var safePackageName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._@/+:-]*$`)
