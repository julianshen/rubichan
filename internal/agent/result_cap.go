package agent

import (
	"fmt"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// applyResultCap truncates a tool result if the tool implements
// agentsdk.ResultCapped and the content exceeds its declared cap.
// Exempt tools (nil, no interface, or cap <= 0) pass through unchanged.
//
// The truncation strategy preserves both the head and tail of the
// content, with a marker inserted between them. This beats head-only
// truncation because tool output often has essential information at
// both ends — shell commands put errors at the tail, file reads put
// context at the head. When the cap is too small to hold the marker
// plus meaningful head+tail slices, the helper falls back to a
// head-only trim.
func applyResultCap(tool any, res agentsdk.ToolResult) agentsdk.ToolResult {
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

	marker := fmt.Sprintf("\n\n[... truncated: %d bytes exceeded %d byte cap ...]\n\n",
		len(res.Content), maxBytes)

	// If the cap is too small to hold the marker plus meaningful
	// head+tail slices, fall back to head-only.
	sliceBudget := maxBytes - len(marker)
	const minSidePerEnd = 100 // bytes
	if sliceBudget < 2*minSidePerEnd {
		if sliceBudget < 0 {
			sliceBudget = 0
		}
		res.Content = res.Content[:sliceBudget] + marker
		return res
	}

	head := sliceBudget / 2
	tail := sliceBudget - head
	res.Content = res.Content[:head] + marker + res.Content[len(res.Content)-tail:]
	return res
}
