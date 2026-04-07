package anthropic

import (
	"encoding/json"

	"github.com/julianshen/rubichan/internal/provider"
)

// Transformer implements provider.MessageTransformer for the Anthropic API.
type Transformer struct{}

// ToProviderJSON converts a CompletionRequest into the Anthropic v1 messages
// JSON request body.
func (t *Transformer) ToProviderJSON(req provider.CompletionRequest) ([]byte, error) {
	apiReq := apiRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		Stream:    true,
	}

	// Build system prompt with optional cache breakpoints.
	if len(req.CacheBreakpoints) > 0 && req.System != "" {
		apiReq.System = buildCachedSystemBlocks(req.System, req.CacheBreakpoints)
	} else {
		apiReq.System = req.System
	}

	if req.Temperature != nil {
		temp := *req.Temperature
		apiReq.Temperature = &temp
	}

	// Convert messages, remapping fields for the Anthropic API.
	for _, msg := range req.Messages {
		apiReq.Messages = append(apiReq.Messages, apiMessage{
			Role:    msg.Role,
			Content: convertContentBlocks(msg.Content),
		})
	}

	// Convert tools.
	for _, tool := range req.Tools {
		apiReq.Tools = append(apiReq.Tools, apiTool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		})
	}

	// Mark last tool with cache_control for Anthropic prompt caching.
	if len(apiReq.Tools) > 0 {
		apiReq.Tools[len(apiReq.Tools)-1].CacheControl = &apiCacheControl{Type: "ephemeral"}
	}

	return json.Marshal(apiReq)
}

// buildCachedSystemBlocks splits the system prompt at breakpoint byte offsets
// and marks pre-breakpoint blocks with cache_control.
func buildCachedSystemBlocks(system string, breakpoints []int) []apiSystemBlock {
	var blocks []apiSystemBlock
	prev := 0
	for _, bp := range breakpoints {
		if bp > len(system) {
			bp = len(system)
		}
		if bp <= prev {
			continue
		}
		blocks = append(blocks, apiSystemBlock{
			Type:         "text",
			Text:         system[prev:bp],
			CacheControl: &apiCacheControl{Type: "ephemeral"},
		})
		prev = bp
	}
	if prev < len(system) {
		blocks = append(blocks, apiSystemBlock{
			Type: "text",
			Text: system[prev:],
		})
	}
	return blocks
}

// convertContentBlocks maps provider.ContentBlock to Anthropic-specific
// apiContentBlock. For tool_result blocks, the text is placed in the "content"
// field (which is what the Anthropic API expects) instead of "text".
func convertContentBlocks(blocks []provider.ContentBlock) []apiContentBlock {
	var out []apiContentBlock
	for _, b := range blocks {
		// Skip empty text blocks — Anthropic rejects them.
		if b.Type == "text" && b.Text == "" {
			continue
		}
		cb := apiContentBlock{
			Type:      b.Type,
			ID:        b.ID,
			Name:      b.Name,
			Input:     b.Input,
			ToolUseID: b.ToolUseID,
			IsError:   b.IsError,
		}
		switch b.Type {
		case "tool_result":
			cb.Content = b.Text
		default:
			cb.Text = b.Text
		}
		out = append(out, cb)
	}
	return out
}
