// Package agentsdk provides the public API for building applications
// on top of Rubichan's agent core.
package agentsdk

import "encoding/json"

// ModelCapabilities describes the per-model capability flags used to tune
// tool dispatch, prompt construction, and agent loop behavior.
type ModelCapabilities struct {
	// SupportsNativeToolUse indicates the model can process a tools[] API parameter.
	SupportsNativeToolUse bool
	// SupportsStreaming indicates the model supports token-level streaming.
	SupportsStreaming bool
	// SupportsSystemPrompt indicates the model accepts a system prompt.
	SupportsSystemPrompt bool
	// NeedsToolDiscoveryHint indicates the system prompt should include a
	// tool inventory section to guide tool selection.
	NeedsToolDiscoveryHint bool
	// MaxToolCount is the maximum number of tools to send to the model.
	// 0 means unlimited.
	MaxToolCount int
	// PreferBatchEdits indicates the model works better with a batch-edit
	// tool rather than individual per-file edit calls.
	PreferBatchEdits bool
}

// CompletionRequest represents a request to an LLM for completion.
type CompletionRequest struct {
	Model            string             `json:"model"`
	System           string             `json:"system,omitempty"`
	Messages         []Message          `json:"messages"`
	Tools            []ToolDef          `json:"tools,omitempty"`
	MaxTokens        int                `json:"max_tokens"`
	Temperature      *float64           `json:"temperature,omitempty"`
	CacheBreakpoints []int              `json:"cache_breakpoints,omitempty"` // byte offsets in System for cache hints
	Capabilities     ModelCapabilities  `json:"capabilities,omitempty"`
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

type contentBlockJSON struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

func (c ContentBlock) MarshalJSON() ([]byte, error) {
	return json.Marshal(contentBlockJSON{
		Type:      c.Type,
		Text:      c.Text,
		ID:        c.ID,
		Name:      c.Name,
		Input:     marshalSafeRawJSON(c.Input),
		ToolUseID: c.ToolUseID,
		IsError:   c.IsError,
	})
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

type toolUseBlockJSON struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

func (t ToolUseBlock) MarshalJSON() ([]byte, error) {
	return json.Marshal(toolUseBlockJSON{
		ID:    t.ID,
		Name:  t.Name,
		Input: marshalSafeRawJSON(t.Input),
	})
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

func marshalSafeRawJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	if json.Valid(raw) {
		return raw
	}

	fallback, err := json.Marshal(string(raw))
	if err != nil {
		return json.RawMessage(`"<invalid-json>"`)
	}
	return json.RawMessage(fallback)
}
