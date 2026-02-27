package provider

import (
	"context"
	"encoding/json"
)

// LLMProvider defines the interface for interacting with an LLM provider.
type LLMProvider interface {
	Stream(ctx context.Context, req CompletionRequest) (<-chan StreamEvent, error)
}

// CompletionRequest represents a request to an LLM for completion.
type CompletionRequest struct {
	Model       string    `json:"model"`
	System      string    `json:"system,omitempty"`
	Messages    []Message `json:"messages"`
	Tools       []ToolDef `json:"tools,omitempty"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature *float64  `json:"temperature,omitempty"`
}

// Message represents a single message in a conversation.
type Message struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

// ContentBlock represents a block of content within a message.
type ContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

// ToolDef defines a tool that can be used by the LLM.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ToolUseBlock represents a tool use block from the LLM response.
type ToolUseBlock struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// StreamEvent represents a single event in a streaming response.
type StreamEvent struct {
	Type         string
	Text         string
	ToolUse      *ToolUseBlock
	Error        error
	InputTokens  int
	OutputTokens int
}

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
