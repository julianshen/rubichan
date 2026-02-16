// internal/output/json_test.go
package output

import (
	"encoding/json"
	"testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONFormatterBasic(t *testing.T) {
	f := NewJSONFormatter()
	result := &RunResult{
		Prompt:    "say hello",
		Response:  "Hello!",
		TurnCount: 1,
		DurationMs: 500,
		Mode:      "generic",
	}

	out, err := f.Format(result)
	require.NoError(t, err)

	var decoded map[string]any
	err = json.Unmarshal(out, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "say hello", decoded["prompt"])
	assert.Equal(t, "Hello!", decoded["response"])
	assert.Equal(t, "generic", decoded["mode"])
	assert.Equal(t, float64(1), decoded["turn_count"])
	assert.Equal(t, float64(500), decoded["duration_ms"])
}

func TestJSONFormatterWithToolCalls(t *testing.T) {
	f := NewJSONFormatter()
	result := &RunResult{
		Prompt:   "read file",
		Response: "contents",
		ToolCalls: []ToolCallLog{
			{Name: "file", Input: json.RawMessage(`{"path":"main.go"}`), Result: "package main"},
		},
		TurnCount: 2,
		DurationMs: 1000,
		Mode:      "generic",
	}

	out, err := f.Format(result)
	require.NoError(t, err)

	var decoded map[string]any
	err = json.Unmarshal(out, &decoded)
	require.NoError(t, err)

	calls, ok := decoded["tool_calls"].([]any)
	require.True(t, ok)
	assert.Len(t, calls, 1)
}

func TestJSONFormatterWithError(t *testing.T) {
	f := NewJSONFormatter()
	result := &RunResult{
		Prompt:    "fail",
		Response:  "",
		TurnCount: 0,
		DurationMs: 0,
		Mode:      "generic",
		Error:     "something went wrong",
	}

	out, err := f.Format(result)
	require.NoError(t, err)

	var decoded map[string]any
	err = json.Unmarshal(out, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "something went wrong", decoded["error"])
}
