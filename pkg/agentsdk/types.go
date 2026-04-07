// Package agentsdk provides the public API for building applications
// on top of Rubichan's agent core.
package agentsdk

import "encoding/json"

// ModelCapabilities describes the per-model capability flags used to tune
// tool dispatch, prompt construction, and agent loop behavior.
//
// When SupportsNativeToolUse is false, the agent renders tool definitions as
// text in the system prompt and parses XML <tool_use> blocks from the model's
// response. In that mode MaxToolCount still limits which tools are rendered,
// but NeedsToolDiscoveryHint has no additional effect since all rendered tools
// are already described in the prompt.
//
// Use DefaultCapabilities to obtain a safe starting point; the zero value
// disables all capabilities, which is almost never desired.
type ModelCapabilities struct {
	// SupportsNativeToolUse indicates the model can process a tools[] API parameter.
	SupportsNativeToolUse bool
	// SupportsSystemPrompt indicates the model accepts a system prompt.
	SupportsSystemPrompt bool
	// NeedsToolDiscoveryHint indicates the system prompt should include a
	// tool inventory section to guide tool selection.
	NeedsToolDiscoveryHint bool
	// MaxToolCount is the maximum number of tools to send to the model.
	// 0 means unlimited. Negative values are treated as 0.
	MaxToolCount int
	// ReasoningEffort controls thinking depth: "low", "medium", "high", or
	// empty (provider default). Mapped to provider-specific parameters when
	// the provider supports extended thinking (e.g. Anthropic budget_tokens).
	ReasoningEffort string
}

// DefaultCapabilities returns ModelCapabilities with the safe defaults:
// native tool use and system prompts enabled, no tool count limit.
// Use DetectCapabilities for model-specific tuning; this function provides
// the base that DetectCapabilities and the Agent constructor both start from.
func DefaultCapabilities() ModelCapabilities {
	return ModelCapabilities{
		SupportsNativeToolUse: true,
		SupportsSystemPrompt:  true,
	}
}

// Provider error kind constants for use with ProviderErrorClassifier.
const (
	ProviderErrContextOverflow = "context overflow"
	ProviderErrRateLimited     = "rate limited"
)

// ProviderErrorClassifier is an optional interface that provider errors can
// implement to expose their classification to the agent loop. The agent uses
// this to emit specific TurnEvent types (e.g. "context_overflow") without
// importing internal provider packages.
type ProviderErrorClassifier interface {
	error
	// ProviderErrorKind returns the error classification string.
	// Known values match the ProviderErr* constants above.
	ProviderErrorKind() string
}

// Content block type constants.
const (
	BlockTypeText       = "text"
	BlockTypeToolUse    = "tool_use"
	BlockTypeToolResult = "tool_result"
	BlockTypeThinking   = "thinking"
)

// Stream event type constants.
const (
	EventTextDelta     = "text_delta"
	EventThinkingDelta = "thinking_delta"
	EventToolUse       = "tool_use"
	EventStop          = "stop"
	EventError         = "error"
)

// CompletionRequest represents a request to an LLM for completion.
type CompletionRequest struct {
	Model            string            `json:"model"`
	System           string            `json:"system,omitempty"`
	Messages         []Message         `json:"messages"`
	Tools            []ToolDef         `json:"tools,omitempty"`
	MaxTokens        int               `json:"max_tokens"`
	Temperature      *float64          `json:"temperature,omitempty"`
	CacheBreakpoints []int             `json:"cache_breakpoints,omitempty"` // byte offsets in System for cache hints
	Capabilities     ModelCapabilities `json:"capabilities,omitempty"`
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
	SearchHint  string          `json:"-"` // keywords for tool_search discovery; never sent to providers
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
