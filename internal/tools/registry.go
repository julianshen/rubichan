package tools

import (
	"fmt"
	"sync"

	"github.com/julianshen/rubichan/internal/provider"
)

// Registry manages a collection of tools. All methods are safe for
// concurrent use.
type Registry struct {
	mu      sync.RWMutex
	tools   map[string]Tool
	aliases map[string]string // alias name -> canonical tool name
}

// NewRegistry creates a new empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools:   make(map[string]Tool),
		aliases: make(map[string]string),
	}
}

// Register adds a tool to the registry. Returns an error if a tool with the
// same name is already registered.
func (r *Registry) Register(t Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if t == nil {
		return fmt.Errorf("cannot register nil tool")
	}
	if _, exists := r.tools[t.Name()]; exists {
		return fmt.Errorf("tool already registered: %s", t.Name())
	}
	r.tools[t.Name()] = t
	return nil
}

// Unregister removes a tool from the registry by name. Returns an error
// if the tool is not registered.
func (r *Registry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[name]; !exists {
		return fmt.Errorf("tool not registered: %s", name)
	}
	delete(r.tools, name)
	return nil
}

// Get retrieves a tool by name. If the name is not a registered tool,
// it checks aliases and resolves to the canonical tool. Returns the tool
// and true if found, or nil and false if not found.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if t, ok := r.tools[name]; ok {
		return t, true
	}
	if canonical, ok := r.aliases[name]; ok {
		t, ok := r.tools[canonical]
		return t, ok
	}
	return nil, false
}

// RegisterAlias maps an alias name to a canonical tool name. When Get is
// called with the alias, it resolves to the canonical tool. Aliases do not
// appear in All() — they are transparent to the model.
//
// Returns an error if the alias shadows a registered canonical tool name
// or if the alias is already registered with a different target.
func (r *Registry) RegisterAlias(alias, canonicalName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[alias]; exists {
		return fmt.Errorf("alias %q shadows canonical tool name", alias)
	}
	if existing, exists := r.aliases[alias]; exists && existing != canonicalName {
		return fmt.Errorf("alias %q already registered for %q", alias, existing)
	}
	r.aliases[alias] = canonicalName
	return nil
}

// Names returns the canonical names of all registered tools.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// All returns provider.ToolDef representations of all registered tools.
func (r *Registry) All() []provider.ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var defs []provider.ToolDef
	for _, t := range r.tools {
		defs = append(defs, provider.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.InputSchema(),
		})
	}
	return defs
}

// Filter creates a new Registry containing only the tools whose names appear
// in the given slice. Unknown names are silently ignored. If names is nil,
// all tools are copied into the new registry. The returned registry is
// independent — registering or unregistering tools in it does not affect
// the original.
func (r *Registry) Filter(names []string) *Registry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	filtered := NewRegistry()
	if names == nil {
		for _, tool := range r.tools {
			_ = filtered.Register(tool)
		}
		r.copyAliasesTo(filtered, nil)
		return filtered
	}

	nameSet := make(map[string]struct{}, len(names))
	for _, n := range names {
		nameSet[n] = struct{}{}
	}
	for name, tool := range r.tools {
		if _, ok := nameSet[name]; ok {
			_ = filtered.Register(tool)
		}
	}
	r.copyAliasesTo(filtered, nameSet)
	return filtered
}

// copyAliasesTo copies aliases whose canonical targets exist in the destination
// registry. If allowedTargets is nil, all aliases are copied.
func (r *Registry) copyAliasesTo(dst *Registry, allowedTargets map[string]struct{}) {
	for alias, canonical := range r.aliases {
		if allowedTargets != nil {
			if _, ok := allowedTargets[canonical]; !ok {
				continue
			}
		}
		dst.aliases[alias] = canonical
	}
}

// SelectForContext returns tool definitions filtered for relevance to the
// current conversation context. Uses keyword heuristics and recent tool
// usage to select relevant tools, falling back to a safe baseline when no
// heuristic matches.
func (r *Registry) SelectForContext(messages []provider.Message) []provider.ToolDef {
	all := r.All()
	selector := NewToolSelector()
	return selector.Select(messages, all)
}

// RegisterDefaultAliases registers common aliases that open-source models
// often hallucinate when attempting to call tools. These map commonly used
// tool names from other frameworks to rubichan's canonical tool names.
func (r *Registry) RegisterDefaultAliases() {
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
		_ = r.RegisterAlias(a[0], a[1])
	}
}
