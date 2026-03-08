package output

import (
	"encoding/json"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stripANSI removes ANSI escape sequences for content comparison.
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

func TestStyledMarkdownFormatterContainsANSI(t *testing.T) {
	f := NewStyledMarkdownFormatter(80)
	result := &RunResult{
		Prompt:     "say hello",
		Response:   "Hello **world**",
		TurnCount:  1,
		DurationMs: 2000,
		Mode:       "generic",
	}

	out, err := f.Format(result)
	require.NoError(t, err)

	s := string(out)
	// ANSI escape codes start with ESC[
	assert.Contains(t, s, "\x1b[", "expected ANSI escape codes in styled output")
	assert.Contains(t, s, "world")
}

func TestStyledMarkdownFormatterPreservesContent(t *testing.T) {
	f := NewStyledMarkdownFormatter(80)
	result := &RunResult{
		Prompt:   "review code",
		Response: "Code looks good.",
		ToolCalls: []ToolCallLog{
			{Name: "file", Input: json.RawMessage(`{}`), Result: "ok"},
		},
		TurnCount:  2,
		DurationMs: 3000,
		Mode:       "generic",
	}

	out, err := f.Format(result)
	require.NoError(t, err)

	s := stripANSI(string(out))
	assert.Contains(t, s, "Code looks good")
	assert.Contains(t, s, "file")
}

func TestStyledMarkdownFormatterError(t *testing.T) {
	f := NewStyledMarkdownFormatter(80)
	result := &RunResult{
		Prompt:     "fail",
		Error:      "timeout exceeded",
		TurnCount:  0,
		DurationMs: 0,
		Mode:       "generic",
	}

	out, err := f.Format(result)
	require.NoError(t, err)

	s := stripANSI(string(out))
	assert.Contains(t, s, "timeout exceeded")
}

func TestStyledMarkdownFormatterNilRendererFallback(t *testing.T) {
	f := &StyledMarkdownFormatter{
		inner:    NewMarkdownFormatter(),
		renderer: nil,
	}
	result := &RunResult{
		Prompt:     "hello",
		Response:   "world",
		TurnCount:  1,
		DurationMs: 500,
		Mode:       "generic",
	}

	out, err := f.Format(result)
	require.NoError(t, err)

	s := string(out)
	// Should fall back to raw markdown (no ANSI codes).
	assert.NotContains(t, s, "\x1b[")
	assert.Contains(t, s, "world")
}

func TestStyledMarkdownFormatterEmpty(t *testing.T) {
	f := NewStyledMarkdownFormatter(80)
	result := &RunResult{
		Prompt:     "hello",
		Response:   "",
		TurnCount:  1,
		DurationMs: 100,
		Mode:       "generic",
	}

	out, err := f.Format(result)
	require.NoError(t, err)
	assert.NotEmpty(t, out)
}
