package agentsdk

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTurnEventTextDelta(t *testing.T) {
	ev := TurnEvent{Type: "text_delta", Text: "hello"}
	assert.Equal(t, "text_delta", ev.Type)
	assert.Equal(t, "hello", ev.Text)
	assert.Nil(t, ev.ToolCall)
}

func TestTurnEventToolCall(t *testing.T) {
	ev := TurnEvent{
		Type: "tool_call",
		ToolCall: &ToolCallEvent{
			ID:    "tc_1",
			Name:  "shell",
			Input: json.RawMessage(`{"command":"ls"}`),
		},
	}
	assert.Equal(t, "shell", ev.ToolCall.Name)
}

func TestTurnEventToolResult(t *testing.T) {
	ev := TurnEvent{
		Type: "tool_result",
		ToolResult: &ToolResultEvent{
			ID:      "tc_1",
			Name:    "shell",
			Content: "file.go",
			IsError: false,
		},
	}
	assert.Equal(t, "file.go", ev.ToolResult.Content)
	assert.False(t, ev.ToolResult.IsError)
}

func TestTurnEventToolProgress(t *testing.T) {
	ev := TurnEvent{
		Type: "tool_progress",
		ToolProgress: &ToolProgressEvent{
			ID:      "tc_1",
			Name:    "shell",
			Stage:   EventDelta,
			Content: "partial output",
		},
	}
	assert.Equal(t, EventDelta, ev.ToolProgress.Stage)
}

func TestTurnEventError(t *testing.T) {
	ev := TurnEvent{Type: "error", Error: errors.New("something failed")}
	assert.EqualError(t, ev.Error, "something failed")
}

func TestTurnEventDone(t *testing.T) {
	ev := TurnEvent{
		Type:         "done",
		InputTokens:  1000,
		OutputTokens: 500,
		DiffSummary:  "modified 3 files",
	}
	assert.Equal(t, 1000, ev.InputTokens)
	assert.Equal(t, "modified 3 files", ev.DiffSummary)
}

func TestTurnEventSubagentDone(t *testing.T) {
	ev := TurnEvent{
		Type: "subagent_done",
		SubagentResult: &SubagentResult{
			Name:      "explorer",
			Output:    "found 3 files",
			TurnCount: 2,
		},
	}
	assert.Equal(t, "explorer", ev.SubagentResult.Name)
}
