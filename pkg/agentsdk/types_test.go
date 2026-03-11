package agentsdk

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessageJSONRoundTrip(t *testing.T) {
	msg := Message{
		Role: "user",
		Content: []ContentBlock{
			{Type: "text", Text: "hello"},
		},
	}

	data, err := json.Marshal(msg)
	require.NoError(t, err)

	var got Message
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, msg, got)
}

func TestContentBlockToolUseRoundTrip(t *testing.T) {
	cb := ContentBlock{
		Type:  "tool_use",
		ID:    "tu_123",
		Name:  "read_file",
		Input: json.RawMessage(`{"path":"/tmp/foo"}`),
	}

	data, err := json.Marshal(cb)
	require.NoError(t, err)

	var got ContentBlock
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, cb, got)
}

func TestContentBlockToolResultRoundTrip(t *testing.T) {
	cb := ContentBlock{
		Type:      "tool_result",
		ToolUseID: "tu_123",
		Text:      "file contents here",
		IsError:   true,
	}

	data, err := json.Marshal(cb)
	require.NoError(t, err)

	var got ContentBlock
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, cb, got)
}

func TestToolDefJSONRoundTrip(t *testing.T) {
	td := ToolDef{
		Name:        "read_file",
		Description: "Read a file",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
	}

	data, err := json.Marshal(td)
	require.NoError(t, err)

	var got ToolDef
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, td, got)
}

func TestToolUseBlockJSONRoundTrip(t *testing.T) {
	tu := ToolUseBlock{
		ID:    "tu_456",
		Name:  "shell",
		Input: json.RawMessage(`{"command":"ls"}`),
	}

	data, err := json.Marshal(tu)
	require.NoError(t, err)

	var got ToolUseBlock
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, tu, got)
}

func TestCompletionRequestJSONRoundTrip(t *testing.T) {
	temp := 0.7
	req := CompletionRequest{
		Model:  "claude-sonnet-4-20250514",
		System: "You are helpful.",
		Messages: []Message{
			{Role: "user", Content: []ContentBlock{{Type: "text", Text: "hi"}}},
		},
		Tools: []ToolDef{
			{Name: "read_file", Description: "Read a file", InputSchema: json.RawMessage(`{}`)},
		},
		MaxTokens:        4096,
		Temperature:      &temp,
		CacheBreakpoints: []int{100, 200},
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var got CompletionRequest
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, req, got)
}

func TestStreamEventFields(t *testing.T) {
	ev := StreamEvent{
		Type:         "text_delta",
		Text:         "hello",
		InputTokens:  100,
		OutputTokens: 50,
	}
	assert.Equal(t, "text_delta", ev.Type)
	assert.Equal(t, "hello", ev.Text)
	assert.Nil(t, ev.ToolUse)
	assert.NoError(t, ev.Error)

	tu := &ToolUseBlock{ID: "tu_1", Name: "shell"}
	ev2 := StreamEvent{Type: "tool_use", ToolUse: tu}
	assert.Equal(t, tu, ev2.ToolUse)
}

func TestCompletionRequestOmitsEmptyFields(t *testing.T) {
	req := CompletionRequest{
		Model:     "claude-sonnet-4-20250514",
		Messages:  []Message{{Role: "user", Content: []ContentBlock{{Type: "text", Text: "hi"}}}},
		MaxTokens: 4096,
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	// Optional fields should be omitted.
	assert.NotContains(t, string(data), "system")
	assert.NotContains(t, string(data), "tools")
	assert.NotContains(t, string(data), "temperature")
	assert.NotContains(t, string(data), "cache_breakpoints")
}
