package agent

import (
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// FilterTools applies an agent definition's tool filter to the full
// tool registry. Supports wildcard mode, explicit allow, and deny sets.
func FilterTools(
	allTools []provider.ToolDef,
	def *agentsdk.AgentDefinition,
	globallyDisallowed []string,
) []provider.ToolDef {
	if def == nil {
		return allTools
	}

	// Build deny set.
	deny := make(map[string]bool)
	for _, t := range globallyDisallowed {
		deny[t] = true
	}
	for _, t := range def.DisallowedTools {
		deny[t] = true
	}

	// Coordinator mode: only coordinator tools.
	if def.IsCoordinator() {
		return filterByNames(allTools, def.Tools, deny)
	}

	// Wildcard mode: all tools except denied.
	if len(def.Tools) == 1 && def.Tools[0] == "*" {
		return filterDenied(allTools, deny)
	}

	// Explicit allow mode.
	return filterByNames(allTools, def.Tools, deny)
}

func filterDenied(all []provider.ToolDef, deny map[string]bool) []provider.ToolDef {
	var out []provider.ToolDef
	for _, t := range all {
		if !deny[t.Name] {
			out = append(out, t)
		}
	}
	return out
}

func filterByNames(all []provider.ToolDef, allow []string, deny map[string]bool) []provider.ToolDef {
	allowSet := make(map[string]bool)
	for _, name := range allow {
		allowSet[name] = true
	}

	var out []provider.ToolDef
	for _, t := range all {
		if allowSet[t.Name] && !deny[t.Name] {
			out = append(out, t)
		}
	}
	return out
}
