package agentsdk

// Conversation manages the message history for an agent session.
type Conversation struct {
	systemPrompt string
	messages     []Message
}

// NewConversation creates a new Conversation with the given system prompt.
func NewConversation(systemPrompt string) *Conversation {
	return &Conversation{systemPrompt: systemPrompt}
}

// SystemPrompt returns the system prompt.
func (c *Conversation) SystemPrompt() string {
	return c.systemPrompt
}

// Messages returns a copy of the current message history.
func (c *Conversation) Messages() []Message {
	return append([]Message(nil), c.messages...)
}

// AddUser appends a user text message.
func (c *Conversation) AddUser(text string) {
	c.messages = append(c.messages, Message{
		Role: "user",
		Content: []ContentBlock{
			{Type: "text", Text: text},
		},
	})
}

// AddAssistant appends an assistant message with the given content blocks.
func (c *Conversation) AddAssistant(blocks []ContentBlock) {
	c.messages = append(c.messages, Message{
		Role:    "assistant",
		Content: blocks,
	})
}

// AddToolResult appends a tool result message.
func (c *Conversation) AddToolResult(toolUseID, content string, isError bool) {
	c.messages = append(c.messages, Message{
		Role: "user",
		Content: []ContentBlock{
			{
				Type:      "tool_result",
				ToolUseID: toolUseID,
				Text:      content,
				IsError:   isError,
			},
		},
	})
}

// Clear removes all messages, preserving the system prompt.
func (c *Conversation) Clear() {
	c.messages = nil
}

// EstimateTokens estimates token count using ~4 chars per token heuristic.
func (c *Conversation) EstimateTokens() int {
	total := len(c.systemPrompt)/4 + 10
	for _, msg := range c.messages {
		for _, block := range msg.Content {
			chars := len(block.Text) + len(block.ID) + len(block.Name) +
				len(block.ToolUseID) + len(block.Input)
			total += chars/4 + 10
		}
	}
	return total
}
