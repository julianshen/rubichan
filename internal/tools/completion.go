package tools

import (
	"context"
	"encoding/json"
)

// TaskCompleteName is the canonical name of the task completion tool.
// Used by the agent loop to detect explicit completion signals.
const TaskCompleteName = "task_complete"

// CompletionSignalTool allows the LLM to explicitly declare task completion.
// When called, the agent loop recognizes this as a stop signal.
type CompletionSignalTool struct{}

// NewCompletionSignalTool creates a new CompletionSignalTool.
func NewCompletionSignalTool() *CompletionSignalTool {
	return &CompletionSignalTool{}
}

func (t *CompletionSignalTool) Name() string { return TaskCompleteName }
func (t *CompletionSignalTool) Description() string {
	return "Signal that the current task is complete. Call this when you have finished all requested work and no further tool calls are needed."
}
func (t *CompletionSignalTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"summary":{"type":"string","description":"Brief summary of what was accomplished"}},"required":["summary"]}`)
}

func (t *CompletionSignalTool) Execute(_ context.Context, input json.RawMessage) (ToolResult, error) {
	var in struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: "invalid input", IsError: true}, nil
	}
	return ToolResult{Content: in.Summary}, nil
}
