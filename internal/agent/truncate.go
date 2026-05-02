package agent

import "fmt"

// truncateHeadTail trims content to maxLen bytes, preserving both head and
// tail with a marker between them. Falls back to head-only when maxLen is
// too small for meaningful head+tail slices.
//
// This strategy preserves context at both ends of tool output — shell
// commands put errors at the tail, file reads put context at the head.
func truncateHeadTail(content string, maxLen int, marker string) string {
	if len(content) <= maxLen {
		return content
	}

	markerLen := len(marker)
	if maxLen <= markerLen {
		return marker[:maxLen]
	}
	if maxLen <= markerLen+100 {
		return content[:max(0, maxLen-markerLen)] + marker
	}

	half := (maxLen - markerLen) / 2
	return content[:half] + marker + content[len(content)-half:]
}

// truncateResultCap is a convenience wrapper for per-tool result caps.
// Builds a marker that includes the original and cap sizes.
func truncateResultCap(content string, maxBytes int) string {
	marker := fmt.Sprintf("\n\n[... truncated: %d bytes exceeded %d byte cap ...]\n\n",
		len(content), maxBytes)
	return truncateHeadTail(content, maxBytes, marker)
}
