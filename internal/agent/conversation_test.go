package agent

import (
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConversationSystemPrompt(t *testing.T) {
	conv := NewConversation("You are a helpful assistant.")
	assert.Equal(t, "You are a helpful assistant.", conv.SystemPrompt())
}

func TestConversationAddUser(t *testing.T) {
	conv := NewConversation("system")
	conv.AddUser("hello")

	msgs := conv.Messages()
	require.Len(t, msgs, 1)
	assert.Equal(t, "user", msgs[0].Role)
	require.Len(t, msgs[0].Content, 1)
	assert.Equal(t, "text", msgs[0].Content[0].Type)
	assert.Equal(t, "hello", msgs[0].Content[0].Text)
}

func TestConversationAddAssistant(t *testing.T) {
	conv := NewConversation("system")
	blocks := []provider.ContentBlock{
		{Type: "text", Text: "Hello there!"},
		{Type: "tool_use", ID: "tool1", Name: "file", Input: []byte(`{"operation":"read"}`)},
	}
	conv.AddAssistant(blocks)

	msgs := conv.Messages()
	require.Len(t, msgs, 1)
	assert.Equal(t, "assistant", msgs[0].Role)
	require.Len(t, msgs[0].Content, 2)
	assert.Equal(t, "text", msgs[0].Content[0].Type)
	assert.Equal(t, "Hello there!", msgs[0].Content[0].Text)
	assert.Equal(t, "tool_use", msgs[0].Content[1].Type)
	assert.Equal(t, "tool1", msgs[0].Content[1].ID)
}

func TestConversationAddToolResult(t *testing.T) {
	conv := NewConversation("system")
	conv.AddToolResult("tool1", "file contents here", false)

	msgs := conv.Messages()
	require.Len(t, msgs, 1)
	assert.Equal(t, "user", msgs[0].Role)
	require.Len(t, msgs[0].Content, 1)
	assert.Equal(t, "tool_result", msgs[0].Content[0].Type)
	assert.Equal(t, "tool1", msgs[0].Content[0].ToolUseID)
	assert.Equal(t, "file contents here", msgs[0].Content[0].Text)
	assert.False(t, msgs[0].Content[0].IsError)
}

func TestConversationAddToolResultError(t *testing.T) {
	conv := NewConversation("system")
	conv.AddToolResult("tool1", "permission denied", true)

	msgs := conv.Messages()
	require.Len(t, msgs, 1)
	assert.True(t, msgs[0].Content[0].IsError)
}

func TestConversationClear(t *testing.T) {
	conv := NewConversation("my system prompt")
	conv.AddUser("hello")
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "hi"}})
	conv.AddUser("how are you?")

	require.Len(t, conv.Messages(), 3)

	conv.Clear()

	assert.Empty(t, conv.Messages())
	assert.Equal(t, "my system prompt", conv.SystemPrompt(), "system prompt should be preserved after clear")
}
