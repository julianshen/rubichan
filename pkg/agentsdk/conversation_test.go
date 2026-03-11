package agentsdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConversationBasicFlow(t *testing.T) {
	c := NewConversation("You are helpful.")
	assert.Equal(t, "You are helpful.", c.SystemPrompt())
	assert.Empty(t, c.Messages())

	c.AddUser("hello")
	assert.Len(t, c.Messages(), 1)
	assert.Equal(t, "user", c.Messages()[0].Role)
	assert.Equal(t, "hello", c.Messages()[0].Content[0].Text)

	c.AddAssistant([]ContentBlock{{Type: "text", Text: "hi there"}})
	assert.Len(t, c.Messages(), 2)
	assert.Equal(t, "assistant", c.Messages()[1].Role)
}

func TestConversationToolResult(t *testing.T) {
	c := NewConversation("")
	c.AddToolResult("tu_1", "file contents", false)

	msg := c.Messages()[0]
	assert.Equal(t, "user", msg.Role)
	assert.Equal(t, "tool_result", msg.Content[0].Type)
	assert.Equal(t, "tu_1", msg.Content[0].ToolUseID)
	assert.False(t, msg.Content[0].IsError)
}

func TestConversationClear(t *testing.T) {
	c := NewConversation("system")
	c.AddUser("msg1")
	c.AddUser("msg2")
	assert.Len(t, c.Messages(), 2)

	c.Clear()
	assert.Empty(t, c.Messages())
	assert.Equal(t, "system", c.SystemPrompt())
}

func TestConversationEstimateTokens(t *testing.T) {
	c := NewConversation("short prompt")
	tokens := c.EstimateTokens()
	assert.Greater(t, tokens, 0)

	c.AddUser("hello world")
	tokensAfter := c.EstimateTokens()
	assert.Greater(t, tokensAfter, tokens)
}
