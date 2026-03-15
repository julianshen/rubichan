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

func TestTurnEventUIRequest(t *testing.T) {
	ev := TurnEvent{
		Type: "ui_request",
		UIRequest: &UIRequest{
			ID:      "req_1",
			Kind:    UIKindApproval,
			Title:   "Approve shell command",
			Message: "Run go test?",
			Actions: []UIAction{
				{ID: "allow", Label: "Allow", Default: true},
				{ID: "deny", Label: "Deny", Style: "danger"},
			},
		},
	}
	assert.Equal(t, "req_1", ev.UIRequest.ID)
	assert.Equal(t, UIKindApproval, ev.UIRequest.Kind)
	assert.Len(t, ev.UIRequest.Actions, 2)
}

func TestTurnEventUIUpdate(t *testing.T) {
	ev := TurnEvent{
		Type: "ui_update",
		UIUpdate: &UIUpdate{
			RequestID: "req_1",
			Status:    "waiting",
			Message:   "Still waiting for response",
		},
	}
	assert.Equal(t, "req_1", ev.UIUpdate.RequestID)
	assert.Equal(t, "waiting", ev.UIUpdate.Status)
}

func TestTurnEventUIResponse(t *testing.T) {
	ev := TurnEvent{
		Type: "ui_response",
		UIResponse: &UIResponse{
			RequestID: "req_1",
			ActionID:  "allow",
			Values:    json.RawMessage(`{"scope":"this_call"}`),
		},
	}
	assert.Equal(t, "allow", ev.UIResponse.ActionID)
	assert.JSONEq(t, `{"scope":"this_call"}`, string(ev.UIResponse.Values))
}
