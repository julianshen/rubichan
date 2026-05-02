package agent

import (
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// applyResultCap truncates a tool result if the tool implements
// agentsdk.ResultCapped and the content exceeds its declared cap.
// Exempt tools (nil, no interface, or cap <= 0) pass through unchanged.
func applyResultCap(tool agentsdk.Tool, res agentsdk.ToolResult) agentsdk.ToolResult {
	capped, ok := tool.(agentsdk.ResultCapped)
	if !ok {
		return res
	}
	maxBytes := capped.MaxResultBytes()
	if maxBytes <= 0 {
		return res
	}
	if len(res.Content) <= maxBytes {
		return res
	}

	res.Content = truncateResultCap(res.Content, maxBytes)
	return res
}
