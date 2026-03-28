package tools

import (
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/internal/provider"
)

const maxDescriptionLen = 120

// DeferralManager holds back tool descriptions that exceed a context budget
// threshold. Deferred tools are discoverable via the Search method.
type DeferralManager struct {
	budgetThresholdPct float64                     // fraction of effective window for tools
	deferredTools      map[string]provider.ToolDef // name -> full definition
}

// NewDeferralManager creates a manager with the given threshold (e.g., 0.10 for 10%).
func NewDeferralManager(thresholdPct float64) *DeferralManager {
	return &DeferralManager{
		budgetThresholdPct: thresholdPct,
		deferredTools:      make(map[string]provider.ToolDef),
	}
}

// estimateToolDefTokens estimates the token count for a single tool definition.
func estimateToolDefTokens(td provider.ToolDef) int {
	return len(td.Name)/4 + len(td.Description)/4 + len(td.InputSchema)/4 + 30
}

// SelectForContext returns tool definitions that fit within the budget.
// Built-in tools (CategoryCore) are always included. MCP and skill tools
// are deferred first when the threshold is exceeded.
func (dm *DeferralManager) SelectForContext(allTools []provider.ToolDef, effectiveWindow int) (active []provider.ToolDef, deferredCount int) {
	dm.deferredTools = make(map[string]provider.ToolDef)
	tokenBudget := int(float64(effectiveWindow) * dm.budgetThresholdPct)

	// Partition tools by category.
	var core, nonCore []provider.ToolDef
	for _, td := range allTools {
		cat := Categorize(td.Name)
		if cat == CategoryCore {
			core = append(core, td)
		} else {
			nonCore = append(nonCore, td)
		}
	}

	// Core tools always active.
	active = append(active, core...)
	usedTokens := 0
	for _, td := range core {
		usedTokens += estimateToolDefTokens(td)
	}

	remaining := tokenBudget - usedTokens

	// Add non-core tools until budget exhausted. Defer the rest.
	for _, td := range nonCore {
		cost := estimateToolDefTokens(td)
		if cost <= remaining {
			active = append(active, td)
			remaining -= cost
		} else {
			dm.deferredTools[td.Name] = td
			deferredCount++
		}
	}

	return active, deferredCount
}

// Search finds deferred tools by name or description keyword match.
func (dm *DeferralManager) Search(query string) []provider.ToolDef {
	query = strings.ToLower(query)
	var results []provider.ToolDef
	for _, td := range dm.deferredTools {
		if strings.Contains(strings.ToLower(td.Name), query) ||
			strings.Contains(strings.ToLower(td.Description), query) {
			results = append(results, td)
		}
	}
	return results
}

// DeferredCount returns the number of currently deferred tools.
func (dm *DeferralManager) DeferredCount() int {
	return len(dm.deferredTools)
}

// truncateToFirstSentence returns the first sentence of s, truncated to maxDescriptionLen.
// A sentence ends at the first '.', '!', or '?' followed by a space or end of string.
func truncateToFirstSentence(s string) string {
	if len(s) == 0 {
		return s
	}
	for i, ch := range s {
		if ch == '.' || ch == '!' || ch == '?' {
			end := i + 1
			if end == len(s) || s[end] == ' ' {
				sentence := s[:end]
				if len(sentence) > maxDescriptionLen {
					return sentence[:maxDescriptionLen]
				}
				return sentence
			}
		}
	}
	// No sentence boundary found — truncate at maxDescriptionLen.
	if len(s) > maxDescriptionLen {
		return s[:maxDescriptionLen]
	}
	return s
}

// ToolSummary generates a human-readable summary of active tools for injection
// into the system prompt. The "Additional tools" paragraph is only included
// when there are deferred tools.
func (dm *DeferralManager) ToolSummary(activeTools []provider.ToolDef) string {
	var sb strings.Builder
	sb.WriteString("## Available Tools\n\nYou have these tools available:\n")
	for _, td := range activeTools {
		desc := truncateToFirstSentence(td.Description)
		fmt.Fprintf(&sb, "- **%s**: %s\n", td.Name, desc)
	}
	if dm.DeferredCount() > 0 {
		sb.WriteString("\nAdditional tools are available but not shown to save context. ")
		sb.WriteString("Use the **tool_search** tool with a keyword query to discover them. ")
		sb.WriteString(`For example: tool_search({"query": "http"}) to find HTTP tools.`)
	}
	return sb.String()
}
