package agent

import "github.com/julianshen/rubichan/internal/provider"

// Conversation manages the message history for an agent session.
type Conversation struct {
	systemPrompt string
	messages     []provider.Message
}

// NewConversation creates a new Conversation with the given system prompt.
func NewConversation(systemPrompt string) *Conversation {
	return &Conversation{
		systemPrompt: systemPrompt,
	}
}

// SystemPrompt returns the system prompt for this conversation.
func (c *Conversation) SystemPrompt() string {
	return c.systemPrompt
}

// Messages returns a copy of all messages in the conversation.
func (c *Conversation) Messages() []provider.Message {
	cp := make([]provider.Message, len(c.messages))
	copy(cp, c.messages)
	return cp
}

// AddUser appends a user message to the conversation.
func (c *Conversation) AddUser(text string) {
	c.messages = append(c.messages, provider.NewUserMessage(text))
}

// AddAssistant appends an assistant message with the given content blocks.
func (c *Conversation) AddAssistant(blocks []provider.ContentBlock) {
	c.messages = append(c.messages, provider.Message{
		Role:    "assistant",
		Content: blocks,
	})
}

// AddToolResult appends a tool result message to the conversation.
func (c *Conversation) AddToolResult(toolUseID, content string, isError bool) {
	c.messages = append(c.messages, provider.NewToolResultMessage(toolUseID, content, isError))
}

// LoadFromMessages replaces the current message history with the given messages.
// The system prompt is preserved. This is used when resuming a saved session.
func (c *Conversation) LoadFromMessages(msgs []provider.Message) {
	c.messages = make([]provider.Message, len(msgs))
	copy(c.messages, msgs)
}

// Clear removes all messages but preserves the system prompt.
func (c *Conversation) Clear() {
	c.messages = nil
}
