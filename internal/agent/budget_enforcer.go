package agent

import (
	"fmt"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// DefaultMaxResultsPerMessageChars is the aggregate budget for all tool
// results within a single assistant message. When exceeded, results are
// offloaded or truncated to fit.
const DefaultMaxResultsPerMessageChars = 200000

// ResultBudgetEnforcer tracks aggregate tool result size per message and
// offloads or truncates results when the total would exceed the budget.
type ResultBudgetEnforcer struct {
	budget int
	used   int
	store  *ResultStore
}

// NewResultBudgetEnforcer creates a budget enforcer with the given aggregate
// budget. A non-positive budget is treated as unlimited (caller should skip
// enforcement instead, but this prevents nil-pointer panics).
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
		return res, nil
	}

	// Result fits within remaining budget.
	be.used += size
	return res, nil
}

// offload stores the result in ResultStore and replaces content with a
// compact reference. The reference size replaces the original in budget
// tracking so the total stays within budget.
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
