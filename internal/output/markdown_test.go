// internal/output/markdown_test.go
package output

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarkdownFormatterBasic(t *testing.T) {
	f := NewMarkdownFormatter()
	result := &RunResult{
		Prompt:    "say hello",
		Response:  "Hello there!",
		TurnCount:  1,
		DurationMs: 2000,
		Mode:       "generic",
	}

	out, err := f.Format(result)
	require.NoError(t, err)

	s := string(out)
	assert.Contains(t, s, "Hello there!")
	assert.Contains(t, s, "1 turn")
	assert.Contains(t, s, "2.0s")
}

func TestMarkdownFormatterWithToolCalls(t *testing.T) {
	f := NewMarkdownFormatter()
	result := &RunResult{
		Prompt:   "read a file",
		Response: "The file contains code.",
		ToolCalls: []ToolCallLog{
			{Name: "file", Input: json.RawMessage(`{"op":"read"}`), Result: "package main", IsError: false},
			{Name: "shell", Input: json.RawMessage(`{"command":"ls"}`), Result: "main.go", IsError: false},
		},
		TurnCount:  3,
		DurationMs: 5000,
		Mode:       "generic",
	}

	out, err := f.Format(result)
	require.NoError(t, err)

	s := string(out)
	assert.Contains(t, s, "Tool Calls")
	assert.Contains(t, s, "file")
	assert.Contains(t, s, "shell")
	assert.Contains(t, s, "3 turns")
}

func TestMarkdownFormatterWithError(t *testing.T) {
	f := NewMarkdownFormatter()
	result := &RunResult{
		Prompt:    "fail",
		Response:  "",
		TurnCount:  0,
		DurationMs: 0,
		Mode:       "generic",
		Error:      "timeout exceeded",
	}

	out, err := f.Format(result)
	require.NoError(t, err)

	s := string(out)
	assert.Contains(t, s, "Error")
	assert.Contains(t, s, "timeout exceeded")
}

func TestMarkdownFormatterNoToolCallsSection(t *testing.T) {
	f := NewMarkdownFormatter()
	result := &RunResult{
		Prompt:    "hello",
		Response:  "Hi!",
		TurnCount:  1,
		DurationMs: 1000,
		Mode:       "generic",
	}

	out, err := f.Format(result)
	require.NoError(t, err)

	s := string(out)
	assert.False(t, strings.Contains(s, "Tool Calls"))
}
