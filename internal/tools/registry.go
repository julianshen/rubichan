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
func (r *Registry) RegisterAlias(alias, canonicalName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.aliases[alias] = canonicalName
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
	return filtered
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
	// Shell tool aliases — models often guess these names.
	r.RegisterAlias("shell_exec", "shell")
	r.RegisterAlias("run_command", "shell")
	r.RegisterAlias("execute", "shell")
	r.RegisterAlias("bash", "shell")
	r.RegisterAlias("terminal", "shell")
	r.RegisterAlias("exec", "shell")

	// File tool aliases — common in other agent frameworks.
	r.RegisterAlias("write_file", "file")
	r.RegisterAlias("read_file", "file")
	r.RegisterAlias("file_write", "file")
	r.RegisterAlias("file_read", "file")
	r.RegisterAlias("edit_file", "file")
	r.RegisterAlias("create_file", "file")

	// Search tool aliases.
	r.RegisterAlias("grep", "search")
	r.RegisterAlias("find", "search")
	r.RegisterAlias("code_search", "search")

	// Process tool aliases.
	r.RegisterAlias("process_manager", "process")
	r.RegisterAlias("bg_process", "process")
}
