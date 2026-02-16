// internal/output/formatter.go
package output

import (
	"encoding/json"
	"time"
)

// RunResult holds the collected output from a headless agent run.
type RunResult struct {
	Prompt    string        `json:"prompt"`
	Response  string        `json:"response"`
	ToolCalls []ToolCallLog `json:"tool_calls,omitempty"`
	TurnCount int           `json:"turn_count"`
	Duration  time.Duration `json:"duration_ms"`
	Mode      string        `json:"mode"`
	Error     string        `json:"error,omitempty"`
}

// ToolCallLog records a single tool invocation during a run.
type ToolCallLog struct {
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input"`
	Result  string          `json:"result"`
	IsError bool            `json:"is_error,omitempty"`
}

// Formatter formats a RunResult into output bytes.
type Formatter interface {
	Format(result *RunResult) ([]byte, error)
}
