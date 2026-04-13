package agentsdk

// ResultCapped is an optional extension interface for tools that want
// the agent to truncate their output before it enters the conversation.
//
// Tools that implement this interface report a byte cap via
// MaxResultBytes. If a result's Content exceeds the cap, the agent
// replaces it with a head+tail slice plus a truncation marker. Tools
// that don't implement this interface are exempt — their output flows
// through unchanged. Implementations may also return a non-positive
// value to opt out at runtime (e.g., disable the cap via config).
//
// Recommended caps:
//   - shell:       64 KB (head + tail of interleaved stdout/stderr)
//   - read_file:   256 KB (large files already have pagination)
//   - grep/search: 64 KB
//   - http_fetch:  128 KB
//
// This matches Claude Code's "Layer 0" context-management step in
// query.ts (applyToolResultBudget). Enforcing size at emission prevents
// a single tool_result from dominating the context window and forcing
// every subsequent compaction pass to trade granular history for one
// bloated result.
type ResultCapped interface {
	// MaxResultBytes returns the maximum byte length of ToolResult.Content.
	// Return a non-positive value to opt out (treated as exempt).
	MaxResultBytes() int
}
