package shell

import (
	"fmt"
	"strings"
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
func writeOutput(w interface{ Write([]byte) (int, error) }, s string) {
	if s == "" {
		return
	}
	fmt.Fprint(w, s)
	if !strings.HasSuffix(s, "\n") {
		fmt.Fprintln(w)
	}
}

// truncateWithNotice truncates s to maxLen bytes, appending a notice if truncated.
func truncateWithNotice(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	remaining := len(s) - maxLen
	return s[:maxLen] + fmt.Sprintf("\n... (truncated, %d more bytes)", remaining)
}
