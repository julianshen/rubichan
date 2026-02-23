// internal/output/json_test.go
package output

import (
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestJSONFormatterBasic(t *testing.T) {
	f := NewJSONFormatter()
	result := &RunResult{
		Prompt:     "say hello",
		Response:   "Hello!",
		TurnCount:  1,
		DurationMs: 500,
		Mode:       "generic",
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
		TurnCount:  2,
		DurationMs: 1000,
		Mode:       "generic",
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
		Prompt:     "fail",
		Response:   "",
		TurnCount:  0,
		DurationMs: 0,
		Mode:       "generic",
		Error:      "something went wrong",
	}

	out, err := f.Format(result)
	require.NoError(t, err)

	var decoded map[string]any
	err = json.Unmarshal(out, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "something went wrong", decoded["error"])
}

func TestJSONFormatterWithSecurityFindings(t *testing.T) {
	f := NewJSONFormatter()
	result := &RunResult{
		Prompt:     "review",
		Response:   "reviewed",
		TurnCount:  1,
		DurationMs: 1000,
		Mode:       "code-review",
		SecurityFindings: []SecurityFinding{
			{ID: "SEC-001", Scanner: "secrets", Severity: "high", Title: "API key exposed", File: "config.go", Line: 10},
		},
		SecuritySummary: &SecuritySummaryData{High: 1},
	}

	out, err := f.Format(result)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(out, &decoded))

	findings, ok := decoded["security_findings"].([]any)
	require.True(t, ok)
	assert.Len(t, findings, 1)

	summary, ok := decoded["security_summary"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(1), summary["high"])
}

func TestJSONFormatterNoSecurityFields(t *testing.T) {
	f := NewJSONFormatter()
	result := &RunResult{
		Prompt:     "hello",
		Response:   "Hi!",
		TurnCount:  1,
		DurationMs: 500,
		Mode:       "generic",
	}

	out, err := f.Format(result)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(out, &decoded))

	_, hasFindings := decoded["security_findings"]
	assert.False(t, hasFindings, "should omit empty security_findings")
	_, hasSummary := decoded["security_summary"]
	assert.False(t, hasSummary, "should omit nil security_summary")
}
