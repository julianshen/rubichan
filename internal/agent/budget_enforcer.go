package agent

import (
	"fmt"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// DefaultMaxResultCharsPerTool is the default per-tool result size limit.
// Matches Claude Code's DEFAULT_MAX_RESULT_SIZE_CHARS=50_000.
const DefaultMaxResultCharsPerTool = 50000

// DefaultMaxResultsPerMessageChars is the aggregate budget for all tool
// results within a single assistant message. When exceeded, the largest
// fresh results are offloaded until the total fits.
// Matches Claude Code's MAX_TOOL_RESULTS_PER_MESSAGE_CHARS=200_000.
const DefaultMaxResultsPerMessageChars = 200000

// ResultBudgetEnforcer tracks aggregate tool result size per message and
// offloads results when the total would exceed the budget.
//
// The enforcer maintains a running total (used) and a list of accepted
// results by size. When a new result would exceed the budget, the largest
// previously accepted results are offloaded first (greedy eviction) to
// minimize the number of offloads.
type ResultBudgetEnforcer struct {
	budget int
	used   int
	store  *ResultStore
	// accepted tracks results that have been counted against the budget.
	// Greedy eviction (makeRoom) needs per-result sizes to find the largest.
	accepted []acceptedResult
}

// acceptedResult tracks a result that has been accepted into the budget.
type acceptedResult struct {
	toolName  string
	toolUseID string
	size      int
}

// NewResultBudgetEnforcer creates a budget enforcer with the given aggregate
// budget in characters. If store is non-nil, oversized results are offloaded
// to the store; otherwise they are truncated in-place.
func NewResultBudgetEnforcer(budget int, store *ResultStore) *ResultBudgetEnforcer {
	if budget <= 0 {
		budget = DefaultMaxResultsPerMessageChars
	}
	return &ResultBudgetEnforcer{
		budget: budget,
		store:  store,
	}
}

// Enforce applies the aggregate budget to a single tool result. If adding
// this result would exceed the budget, it attempts to offload or truncate.
// Returns the (possibly modified) result that should be added to the message.
func (be *ResultBudgetEnforcer) Enforce(toolName, toolUseID string, res agentsdk.ToolResult) (agentsdk.ToolResult, error) {
	size := len(res.Content)

	// Check if adding this result would exceed the remaining budget.
	remaining := be.budget - be.used
	if remaining <= 0 {
		// Budget exhausted — must offload or truncate to zero.
		if be.store != nil {
			return be.offload(toolName, toolUseID, res)
		}
		res.Content = be.truncate(res.Content, 0)
		return res, nil
	}

	if size > remaining {
		// Result exceeds remaining budget.
		if be.store != nil {
			return be.offload(toolName, toolUseID, res)
		}
		// No store: truncate to fit remaining budget.
		res.Content = be.truncate(res.Content, remaining)
		be.used += len(res.Content)
		be.accepted = append(be.accepted, acceptedResult{toolName: toolName, toolUseID: toolUseID, size: len(res.Content)})
		return res, nil
	}

	// Result fits within remaining budget.
	be.used += size
	be.accepted = append(be.accepted, acceptedResult{toolName: toolName, toolUseID: toolUseID, size: size})
	return res, nil
}

// makeRoom is no longer used — the simplified Enforce handles budget
// exhaustion by checking remaining budget per result.
// Kept for backward compatibility; will be removed in a follow-up.
//
//nolint:unused
func (be *ResultBudgetEnforcer) makeRoom(needed int) {
}

// offload stores the result in ResultStore and replaces content with a
// compact reference. The original content is removed from budget tracking.
func (be *ResultBudgetEnforcer) offload(toolName, toolUseID string, res agentsdk.ToolResult) (agentsdk.ToolResult, error) {
	if be.store == nil {
		return res, nil
	}
	ref, err := be.store.OffloadResult(toolName, toolUseID, res.Content)
	if err != nil {
		// Graceful degradation: return original if offload fails.
		// The error is returned to the caller (agent.go logs it).
		return res, fmt.Errorf("offload failed: %w", err)
	}
	res.Content = ref
	be.used += len(ref)
	return res, nil
}

// truncate trims content to maxLen bytes, preserving head and tail with
// a marker. Falls back to head-only if maxLen is too small.
func (be *ResultBudgetEnforcer) truncate(content string, maxLen int) string {
	marker := fmt.Sprintf("\n\n[... truncated: %d chars exceeded budget ...]\n\n", len(content))
	return truncateHeadTail(content, maxLen, marker)
}
