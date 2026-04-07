package openai

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/provider/normalize"
)

// API wire-format types for OpenAI Chat Completions endpoint.

type apiRequest struct {
	Model       string       `json:"model"`
	Messages    []apiMessage `json:"messages"`
	Tools       []apiTool    `json:"tools,omitempty"`
	MaxTokens   int          `json:"max_tokens"`
	Temperature *float64     `json:"temperature,omitempty"`
	Stream      bool         `json:"stream"`
}

type apiMessage struct {
	Role       string        `json:"role"`
	Content    any           `json:"content,omitempty"`
	ToolCalls  []apiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type apiTool struct {
	Type     string      `json:"type"`
	Function apiFunction `json:"function"`
}

type apiFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type apiToolCall struct {
	ID       string      `json:"id"`
	Type     string      `json:"type"`
	Function apiCallFunc `json:"function"`
}

type apiCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Quirks configures provider-specific behavioral adjustments for
// OpenAI-compatible APIs that deviate from the standard format.
type Quirks struct {
	// MaxToolIDLength truncates tool IDs to this length (0 = no limit).
	MaxToolIDLength int
	// AlphanumericToolIDs restricts tool IDs to [a-zA-Z0-9_-].
	AlphanumericToolIDs bool
	// InsertAssistantAfterTool inserts a filler assistant message between
	// tool results and user messages for providers that require it.
	InsertAssistantAfterTool bool
}

// Transformer implements provider.MessageTransformer for OpenAI-compatible APIs.
type Transformer struct {
	Quirks Quirks
}

// ToProviderJSON converts a CompletionRequest into the OpenAI Chat Completions
// JSON request body.
func (t *Transformer) ToProviderJSON(req provider.CompletionRequest) ([]byte, error) {
	apiReq := apiRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		Stream:    true,
	}

	if req.Temperature != nil {
		temp := *req.Temperature
		apiReq.Temperature = &temp
	}

	// Add system message if present.
	if req.System != "" {
		apiReq.Messages = append(apiReq.Messages, apiMessage{
			Role:    "system",
			Content: req.System,
		})
	}

	// Normalize messages.
	messages := normalize.RemoveEmptyMessages(req.Messages)
	if t.Quirks.AlphanumericToolIDs || t.Quirks.MaxToolIDLength > 0 {
		scrub := func(id string) string {
			if t.Quirks.AlphanumericToolIDs {
				id = normalize.ScrubAnthropicToolID(id)
			}
			return normalize.TruncateToolID(id, t.Quirks.MaxToolIDLength)
		}
		messages = normalize.ScrubToolIDs(messages, scrub)
	}
	if t.Quirks.InsertAssistantAfterTool {
		messages = normalize.InsertAssistantBetweenToolAndUser(messages)
	}

	// Convert messages.
	for _, msg := range messages {
		apiReq.Messages = append(apiReq.Messages, convertMessages(msg)...)
	}

	// Convert tools.
	for _, tool := range req.Tools {
		apiReq.Tools = append(apiReq.Tools, apiTool{
			Type: "function",
			Function: apiFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		})
	}

	// Sort tools alphabetically for deterministic serialization (OpenAI auto-cache optimization).
	sort.Slice(apiReq.Tools, func(i, j int) bool {
		return apiReq.Tools[i].Function.Name < apiReq.Tools[j].Function.Name
	})

	return json.Marshal(apiReq)
}

// convertMessages converts a single provider.Message to one or more apiMessages.
// A user message with multiple tool_result blocks produces one apiMessage per result.
func convertMessages(msg provider.Message) []apiMessage {
	switch msg.Role {
	case "assistant":
		return []apiMessage{convertAssistantMessage(msg)}
	case "user":
		return convertUserMessages(msg)
	default:
		var texts []string
		for _, block := range msg.Content {
			if block.Type == "text" {
				texts = append(texts, block.Text)
			}
		}
		return []apiMessage{{
			Role:    msg.Role,
			Content: strings.Join(texts, ""),
		}}
	}
}

func convertAssistantMessage(msg provider.Message) apiMessage {
	var text string
	var toolCalls []apiToolCall

	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				text += block.Text
			}
		case "tool_use":
			toolCalls = append(toolCalls, apiToolCall{
				ID:   block.ID,
				Type: "function",
				Function: apiCallFunc{
					Name:      block.Name,
					Arguments: string(block.Input),
				},
			})
		}
	}

	apiMsg := apiMessage{
		Role: "assistant",
	}
	if text != "" || len(toolCalls) > 0 {
		apiMsg.Content = text
	}
	if len(toolCalls) > 0 {
		apiMsg.ToolCalls = toolCalls
	}

	return apiMsg
}

// convertUserMessages handles user messages that may contain multiple tool_result
// blocks. Each tool_result becomes a separate "tool" message in the OpenAI format.
func convertUserMessages(msg provider.Message) []apiMessage {
	var toolResults []apiMessage
	var texts []string

	for _, block := range msg.Content {
		switch block.Type {
		case "tool_result":
			toolResults = append(toolResults, apiMessage{
				Role:       "tool",
				Content:    block.Text,
				ToolCallID: block.ToolUseID,
			})
		case "text":
			if block.Text != "" {
				texts = append(texts, block.Text)
			}
		}
	}

	if len(toolResults) > 0 {
		return toolResults
	}

	return []apiMessage{{
		Role:    "user",
		Content: strings.Join(texts, ""),
	}}
}
