package provider

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewUserMessage(t *testing.T) {
	msg := NewUserMessage("hello world")

	assert.Equal(t, "user", msg.Role)
	require.Len(t, msg.Content, 1)
	assert.Equal(t, "text", msg.Content[0].Type)
	assert.Equal(t, "hello world", msg.Content[0].Text)
}

func TestNewUserMessageEmpty(t *testing.T) {
	msg := NewUserMessage("")

	assert.Equal(t, "user", msg.Role)
	require.Len(t, msg.Content, 1)
	assert.Equal(t, "text", msg.Content[0].Type)
	assert.Equal(t, "", msg.Content[0].Text)
}

func TestNewToolResultMessage(t *testing.T) {
	msg := NewToolResultMessage("tool-123", "result content", false)

	assert.Equal(t, "user", msg.Role)
	require.Len(t, msg.Content, 1)
	assert.Equal(t, "tool_result", msg.Content[0].Type)
	assert.Equal(t, "tool-123", msg.Content[0].ToolUseID)
	assert.Equal(t, "result content", msg.Content[0].Text)
	assert.False(t, msg.Content[0].IsError)
}

func TestNewToolResultMessageWithError(t *testing.T) {
	msg := NewToolResultMessage("tool-456", "error occurred", true)

	assert.Equal(t, "user", msg.Role)
	require.Len(t, msg.Content, 1)
	assert.Equal(t, "tool_result", msg.Content[0].Type)
	assert.Equal(t, "tool-456", msg.Content[0].ToolUseID)
	assert.Equal(t, "error occurred", msg.Content[0].Text)
	assert.True(t, msg.Content[0].IsError)
}

func TestToolDefJSON(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)
	tool := ToolDef{
		Name:        "read_file",
		Description: "Reads a file from disk",
		InputSchema: schema,
	}

	data, err := json.Marshal(tool)
	require.NoError(t, err)

	var decoded ToolDef
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "read_file", decoded.Name)
	assert.Equal(t, "Reads a file from disk", decoded.Description)
	assert.JSONEq(t, `{"type":"object","properties":{"path":{"type":"string"}}}`, string(decoded.InputSchema))
}

func TestCompletionRequestJSON(t *testing.T) {
	req := CompletionRequest{
		Model:  "claude-sonnet-4-5",
		System: "You are a helpful assistant.",
		Messages: []Message{
			NewUserMessage("hello"),
		},
		MaxTokens:   4096,
		Temperature: 0.7,
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var decoded CompletionRequest
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "claude-sonnet-4-5", decoded.Model)
	assert.Equal(t, "You are a helpful assistant.", decoded.System)
	assert.Len(t, decoded.Messages, 1)
	assert.Equal(t, 4096, decoded.MaxTokens)
	assert.Equal(t, 0.7, decoded.Temperature)
}

func TestStreamEventTypes(t *testing.T) {
	// Test text delta event
	textEvt := StreamEvent{
		Type: "text_delta",
		Text: "Hello",
	}
	assert.Equal(t, "text_delta", textEvt.Type)
	assert.Equal(t, "Hello", textEvt.Text)
	assert.Nil(t, textEvt.ToolUse)
	assert.Nil(t, textEvt.Error)

	// Test tool use event
	toolEvt := StreamEvent{
		Type: "tool_use",
		ToolUse: &ToolUseBlock{
			ID:    "tool-1",
			Name:  "read_file",
			Input: json.RawMessage(`{"path":"/tmp/test"}`),
		},
	}
	assert.Equal(t, "tool_use", toolEvt.Type)
	require.NotNil(t, toolEvt.ToolUse)
	assert.Equal(t, "tool-1", toolEvt.ToolUse.ID)
	assert.Equal(t, "read_file", toolEvt.ToolUse.Name)

	// Test error event
	errEvt := StreamEvent{
		Type:  "error",
		Error: assert.AnError,
	}
	assert.Equal(t, "error", errEvt.Type)
	assert.Error(t, errEvt.Error)

	// Test stop event
	stopEvt := StreamEvent{
		Type: "stop",
	}
	assert.Equal(t, "stop", stopEvt.Type)
}

func TestContentBlockJSON(t *testing.T) {
	// Test text block serialization
	textBlock := ContentBlock{
		Type: "text",
		Text: "hello world",
	}
	data, err := json.Marshal(textBlock)
	require.NoError(t, err)

	var decoded ContentBlock
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, "text", decoded.Type)
	assert.Equal(t, "hello world", decoded.Text)

	// Test tool_use block serialization
	toolBlock := ContentBlock{
		Type:  "tool_use",
		ID:    "tool-1",
		Name:  "read_file",
		Input: json.RawMessage(`{"path":"/tmp"}`),
	}
	data, err = json.Marshal(toolBlock)
	require.NoError(t, err)

	var decodedTool ContentBlock
	err = json.Unmarshal(data, &decodedTool)
	require.NoError(t, err)
	assert.Equal(t, "tool_use", decodedTool.Type)
	assert.Equal(t, "tool-1", decodedTool.ID)
	assert.Equal(t, "read_file", decodedTool.Name)

	// Test tool_result block serialization
	resultBlock := ContentBlock{
		Type:      "tool_result",
		ToolUseID: "tool-1",
		Text:      "file contents",
		IsError:   false,
	}
	data, err = json.Marshal(resultBlock)
	require.NoError(t, err)

	var decodedResult ContentBlock
	err = json.Unmarshal(data, &decodedResult)
	require.NoError(t, err)
	assert.Equal(t, "tool_result", decodedResult.Type)
	assert.Equal(t, "tool-1", decodedResult.ToolUseID)
	assert.Equal(t, "file contents", decodedResult.Text)
	assert.False(t, decodedResult.IsError)
}
