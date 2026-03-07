package agent

import (
	"fmt"
	"sort"
	"sync"
)

// AgentDef describes a pre-configured subagent template. It captures the
// identity, capabilities, and constraints of a specialized agent that can
// be spawned by the orchestrator.
type AgentDef struct {
	Name          string   `toml:"name" yaml:"name"`
	Description   string   `toml:"description" yaml:"description"`
	SystemPrompt  string   `toml:"system_prompt" yaml:"system_prompt"`
	Tools         []string `toml:"tools" yaml:"tools"`
	MaxTurns      int      `toml:"max_turns" yaml:"max_turns"`
	MaxDepth      int      `toml:"max_depth" yaml:"max_depth"`
	Model         string   `toml:"model" yaml:"model"`
	InheritSkills *bool    `toml:"inherit_skills" yaml:"inherit_skills"`
	ExtraSkills   []string `toml:"extra_skills" yaml:"extra_skills"`
	DisableSkills []string `toml:"disable_skills" yaml:"disable_skills"`
	Isolation     string   `toml:"isolation" yaml:"isolation"` // "", "worktree"
}

// AgentDefRegistry is a thread-safe registry of named agent definitions.
type AgentDefRegistry struct {
	mu   sync.RWMutex
	defs map[string]*AgentDef
}

// NewAgentDefRegistry creates an empty registry.
func NewAgentDefRegistry() *AgentDefRegistry {
	return &AgentDefRegistry{defs: make(map[string]*AgentDef)}
}

// Register adds an agent definition. Returns an error if the name is
// already registered.
func (r *AgentDefRegistry) Register(def *AgentDef) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.defs[def.Name]; ok {
		return fmt.Errorf("agent definition %q already registered", def.Name)
	}
	r.defs[def.Name] = def
	return nil
}

// Get retrieves an agent definition by name. The boolean indicates whether
// the definition was found.
func (r *AgentDefRegistry) Get(name string) (*AgentDef, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.defs[name]
	return def, ok
}

// All returns every registered agent definition, sorted by name.
func (r *AgentDefRegistry) All() []*AgentDef {
	r.mu.RLock()
	result := make([]*AgentDef, 0, len(r.defs))
	for _, def := range r.defs {
		result = append(result, def)
	}
	r.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// Unregister removes an agent definition by name. Returns an error if the
// name is not found.
func (r *AgentDefRegistry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.defs[name]; !ok {
		return fmt.Errorf("agent definition %q not found", name)
	}
	delete(r.defs, name)
	return nil
}
