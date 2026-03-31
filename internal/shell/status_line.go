package shell

import (
	"fmt"
	"strings"
)

// ANSI color codes
const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiGreen  = "\033[32m"
	ansiRed    = "\033[31m"
	ansiCyan   = "\033[36m"
	ansiYellow = "\033[33m"
)

// StatusLine renders a rich status display as part of the shell prompt.
type StatusLine struct {
	segments map[string]string
	width    int
	enabled  bool
	homeDir  string
}

// NewStatusLine creates a status line with the given terminal width.
func NewStatusLine(width int) *StatusLine {
	return &StatusLine{
		segments: make(map[string]string),
		width:    width,
		enabled:  true,
	}
}

// Update changes a segment's value.
func (sl *StatusLine) Update(key string, value string) {
	if sl == nil || !sl.enabled {
		return
	}
	sl.segments[key] = value
}

// UpdateCWD updates the working directory segment, shortening home to ~.
func (sl *StatusLine) UpdateCWD(path string) {
	if sl == nil {
		return
	}
	display := path
	if sl.homeDir != "" && strings.HasPrefix(path, sl.homeDir) {
		rest := path[len(sl.homeDir):]
		if rest == "" {
			display = "~"
		} else if rest[0] == '/' {
			display = "~" + rest
		}
	}
	sl.Update("cwd", display)
}

// UpdateExitCode updates the exit code segment with appropriate symbol.
func (sl *StatusLine) UpdateExitCode(exitCode int) {
	if sl == nil {
		return
	}
	if exitCode == 0 {
		sl.Update("exitcode", "0")
	} else {
		sl.Update("exitcode", fmt.Sprintf("%d", exitCode))
	}
}

// UpdateModel updates the model segment with a shortened model name.
func (sl *StatusLine) UpdateModel(model string) {
	if sl == nil {
		return
	}
	sl.Update("model", shortenModelName(model))
}

// Render returns the formatted status line string with ANSI colors.
func (sl *StatusLine) Render() string {
	if sl == nil || !sl.enabled {
		return ""
	}

	var parts []string

	// CWD
	if cwd := sl.segments["cwd"]; cwd != "" {
		parts = append(parts, ansiCyan+cwd+ansiReset)
	}

	// Git branch
	if branch := sl.segments["branch"]; branch != "" {
		parts = append(parts, ansiYellow+branch+ansiReset)
	}

	// Exit code
	if ec := sl.segments["exitcode"]; ec != "" {
		if ec == "0" {
			parts = append(parts, ansiGreen+"✓"+ansiReset)
		} else {
			parts = append(parts, ansiRed+"✗ "+ec+ansiReset)
		}
	}

	// Model
	if model := sl.segments["model"]; model != "" {
		parts = append(parts, ansiDim+model+ansiReset)
	}

	if len(parts) == 0 {
		return ""
	}

	line := strings.Join(parts, " | ")

	// Truncate visible content to terminal width
	if sl.width > 0 {
		visible := stripANSI(line)
		if len(visible) > sl.width {
			// Truncate (rough — cut visible chars then re-add reset)
			line = truncateVisible(line, sl.width-3) + "..." + ansiReset
		}
	}

	return line
}

// shortenModelName extracts the model family name from a full model ID.
func shortenModelName(model string) string {
	// claude-sonnet-4-5 → sonnet, claude-opus-4-6 → opus, claude-haiku-4-5 → haiku
	families := []string{"opus", "sonnet", "haiku"}
	lower := strings.ToLower(model)
	for _, f := range families {
		if strings.Contains(lower, f) {
			return f
		}
	}
	return model
}

// stripANSI removes ANSI escape sequences from a string.
func stripANSI(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		if r == '\033' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// truncateVisible truncates a string with ANSI codes to maxVisible visible characters.
func truncateVisible(s string, maxVisible int) string {
	var b strings.Builder
	inEscape := false
	visible := 0
	for _, r := range s {
		if r == '\033' {
			inEscape = true
			b.WriteRune(r)
			continue
		}
		if inEscape {
			b.WriteRune(r)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		if visible >= maxVisible {
			break
		}
		b.WriteRune(r)
		visible++
	}
	return b.String()
}
