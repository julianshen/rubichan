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
func NewUserMessage(text string) Message {
	return Message{
		Role: "user",
		Content: []ContentBlock{
			{
				Type: "text",
				Text: text,
			},
		},
	}
}

// NewToolResultMessage creates a new tool result message.
func NewToolResultMessage(toolUseID, content string, isError bool) Message {
	return Message{
		Role: "user",
		Content: []ContentBlock{
			{
				Type:      "tool_result",
				ToolUseID: toolUseID,
				Text:      content,
				IsError:   isError,
			},
		},
	}
}
