package provider

import (
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// Type aliases — all existing code using provider.Message etc. compiles unchanged.

// LLMProvider defines the interface for interacting with an LLM provider.
type LLMProvider = agentsdk.LLMProvider

// CompletionRequest represents a request to an LLM for completion.
type CompletionRequest = agentsdk.CompletionRequest

// Message represents a single message in a conversation.
type Message = agentsdk.Message

// ContentBlock represents a block of content within a message.
type ContentBlock = agentsdk.ContentBlock

// ToolDef defines a tool that can be used by the LLM.
type ToolDef = agentsdk.ToolDef

// ToolUseBlock represents a tool use block from the LLM response.
type ToolUseBlock = agentsdk.ToolUseBlock

// StreamEvent represents a single event in a streaming response.
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
