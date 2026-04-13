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

// ConcurrencySafeTool is an optional extension interface. Tools that
// implement it declare themselves safe to execute as soon as their
// tool_use block finalizes during streaming — the agent dispatches
// them without waiting for the full model response.
//
// A tool is concurrency-safe if and only if:
//  1. It has no observable side effects on the filesystem, network,
//     or process state (pure reads).
//  2. Its result depends only on the input and on external state that
//     won't be mutated by another concurrently-dispatched tool.
//  3. Re-ordering it with respect to sibling tool calls in the same
//     response is a no-op as far as the user is concerned.
//
// Tools that return false (or don't implement the interface) are
// queued and executed after the stream completes, in declaration
// order, via the normal executeTools pipeline.
//
// Examples of safe tools: read_file, grep, glob, list_dir, code_search,
// http_get.
//
// Examples of unsafe tools: write_file, patch_file, shell, edit, git,
// database writes, anything with network side effects.
//
// This matches Claude Code's StreamingToolExecutor in query.ts: "A tool
// is dispatched as soon as its tool_use block is finalized during the
// stream; by the time the model finishes post-tool text, the file
// contents are already in memory."
type ConcurrencySafeTool interface {
	IsConcurrencySafe() bool
}
