// internal/output/formatter_test.go
package output

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRunResultToolCallLogJSON(t *testing.T) {
	r := RunResult{
		Prompt:    "hello",
		Response:  "world",
		TurnCount: 1,
		Duration:  2 * time.Second,
		Mode:      "generic",
		ToolCalls: []ToolCallLog{
			{Name: "file", Input: json.RawMessage(`{"op":"read"}`), Result: "ok", IsError: false},
		},
	}

	assert.Equal(t, "hello", r.Prompt)
	assert.Equal(t, "world", r.Response)
	assert.Len(t, r.ToolCalls, 1)
	assert.Equal(t, "file", r.ToolCalls[0].Name)
}
