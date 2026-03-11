// Package agentsdk provides the public API for building applications
// on top of Rubichan's agent core.
package agentsdk

import "encoding/json"

// CompletionRequest represents a request to an LLM for completion.
type CompletionRequest struct {
	Model            string    `json:"model"`
	System           string    `json:"system,omitempty"`
	Messages         []Message `json:"messages"`
	Tools            []ToolDef `json:"tools,omitempty"`
	MaxTokens        int       `json:"max_tokens"`
	Temperature      *float64  `json:"temperature,omitempty"`
	CacheBreakpoints []int     `json:"cache_breakpoints,omitempty"` // byte offsets in System for cache hints
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
