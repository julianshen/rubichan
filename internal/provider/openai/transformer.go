package openai

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/julianshen/rubichan/internal/provider"
)

// Transformer implements provider.MessageTransformer for OpenAI-compatible APIs.
type Transformer struct{}

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

	// Convert messages.
	for _, msg := range req.Messages {
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
