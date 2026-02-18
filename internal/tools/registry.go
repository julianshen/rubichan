package tools

import (
	"fmt"
	"sync"

	"github.com/julianshen/rubichan/internal/provider"
)

// Registry manages a collection of tools. All methods are safe for
// concurrent use.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry creates a new empty tool registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
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

// Get retrieves a tool by name. Returns the tool and true if found,
// or nil and false if not found.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	t, ok := r.tools[name]
	return t, ok
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
