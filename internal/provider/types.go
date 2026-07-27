package provider

import (
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// Type aliases — all existing code using provider.Message etc. compiles unchanged.
// Canonical definitions live in pkg/agentsdk/.

type LLMProvider = agentsdk.LLMProvider
type CompletionRequest = agentsdk.CompletionRequest
type Message = agentsdk.Message
type ContentBlock = agentsdk.ContentBlock
type ToolDef = agentsdk.ToolDef
type ToolUseBlock = agentsdk.ToolUseBlock
type StreamEvent = agentsdk.StreamEvent

// NewUserMessage creates a new user message with a single text content block.
// Delegates to the canonical constructor so this core and the portable one
// cannot build different shapes for the same message.
func NewUserMessage(text string) Message {
	return agentsdk.NewUserMessage(text)
}

// NewToolResultMessage creates a new tool result message. Delegates to the
// canonical constructor; see agentsdk.NewToolResultMessage.
func NewToolResultMessage(toolUseID, content string, isError bool) Message {
	return agentsdk.NewToolResultMessage(toolUseID, content, isError)
}
