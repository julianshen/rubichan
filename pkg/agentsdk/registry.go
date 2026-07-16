package agentsdk

import (
	"fmt"
	"sort"
	"sync"
)

// Registry manages a collection of tools. All methods are safe for
// concurrent use. This is a standalone implementation with no internal
// package dependencies.
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
	name := t.Name()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool already registered: %s", name)
	}
	r.tools[name] = t
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

// All returns ToolDef representations of all registered tools,
// sorted alphabetically by name for deterministic ordering. Tools
// implementing SearchHinter contribute their hint to the definition.
func (r *Registry) All() []ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]ToolDef, 0, len(r.tools))
	for _, t := range r.tools {
		td := ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.InputSchema(),
		}
		if hinter, ok := t.(SearchHinter); ok {
			td.SearchHint = hinter.SearchHint()
		}
		defs = append(defs, td)
	}
	sort.Slice(defs, func(i, j int) bool {
		return defs[i].Name < defs[j].Name
	})
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

// Count returns the number of registered tools.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}
