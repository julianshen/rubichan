package tools

import (
	"fmt"

	"github.com/julianshen/rubichan/internal/provider"
)

// Registry manages a collection of tools.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry creates a new empty tool registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool to the registry. Returns an error if a tool with the
// same name is already registered.
func (r *Registry) Register(t Tool) error {
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
	if _, exists := r.tools[name]; !exists {
		return fmt.Errorf("tool not registered: %s", name)
	}
	delete(r.tools, name)
	return nil
}

// Get retrieves a tool by name. Returns the tool and true if found,
// or nil and false if not found.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// All returns provider.ToolDef representations of all registered tools.
func (r *Registry) All() []provider.ToolDef {
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
