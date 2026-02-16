// internal/output/formatter_test.go
package output

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRunResultToolCallLogJSON(t *testing.T) {
	r := RunResult{
		Prompt:    "hello",
		Response:  "world",
		TurnCount:  1,
		DurationMs: 2000,
		Mode:       "generic",
		ToolCalls: []ToolCallLog{
			{Name: "file", Input: json.RawMessage(`{"op":"read"}`), Result: "ok", IsError: false},
		},
	}

	assert.Equal(t, "hello", r.Prompt)
	assert.Equal(t, "world", r.Response)
	assert.Len(t, r.ToolCalls, 1)
	assert.Equal(t, "file", r.ToolCalls[0].Name)
}
