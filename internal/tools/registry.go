package tools

import (
	"log"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// Registry is the canonical tool registry, defined in pkg/agentsdk so that
// external embedders and the internal agent share one implementation
// (Phase 0 of docs/MODULAR_CORE_REDESIGN.md). The alias keeps existing
// code using tools.Registry compiling unchanged.
type Registry = agentsdk.Registry

// NewRegistry creates a new empty tool registry.
func NewRegistry() *Registry {
	return agentsdk.NewRegistry()
}

// SelectForContext returns tool definitions filtered for relevance to the
// current conversation context. Uses keyword heuristics and recent tool
// usage to select relevant tools, falling back to a safe baseline when no
// heuristic matches.
//
// This is a package function rather than a Registry method because the
// selection heuristic is rubichan product policy, while Registry itself
// lives in the public SDK.
func SelectForContext(r *Registry, messages []provider.Message) []provider.ToolDef {
	all := r.All()
	selector := NewToolSelector()
	return selector.Select(messages, all)
}

// RegisterDefaultAliases registers common aliases that open-source models
// often hallucinate when attempting to call tools. These map commonly used
// tool names from other frameworks to rubichan's canonical tool names.
//
// Like SelectForContext, this alias table is rubichan product policy and
// therefore stays out of the SDK Registry.
func RegisterDefaultAliases(r *Registry) {
	aliases := [][2]string{
		// Shell tool aliases — models often guess these names.
		{"shell_exec", "shell"},
		{"run_command", "shell"},
		{"execute", "shell"},
		{"bash", "shell"},
		{"terminal", "shell"},
		{"exec", "shell"},
		{"tool_shell", "shell"},
		{"execute_command", "shell"},

		// File tool aliases — common in other agent frameworks.
		{"write_file", "file"},
		{"read_file", "file"},
		{"file_write", "file"},
		{"file_read", "file"},
		{"edit_file", "file"},
		{"create_file", "file"},
		{"tool_file", "file"},

		// Search tool aliases.
		{"grep", "search"},
		{"find", "search"},
		{"code_search", "search"},

		// Process tool aliases.
		{"process_manager", "process"},
		{"bg_process", "process"},
		{"tool_process", "process"},
	}
	for _, a := range aliases {
		if err := r.RegisterAlias(a[0], a[1]); err != nil {
			log.Printf("warning: alias %q -> %q: %v", a[0], a[1], err)
		}
	}
}
