package agent

import (
	"fmt"
	"sync"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// AgentRegistry manages built-in and custom agent definitions.
type AgentRegistry struct {
	mu       sync.RWMutex
	agents   map[string]*agentsdk.AgentDefinition
	builtIns map[string]*agentsdk.AgentDefinition
}

// NewAgentRegistry creates a registry with built-in definitions.
func NewAgentRegistry() *AgentRegistry {
	r := &AgentRegistry{
		agents:   make(map[string]*agentsdk.AgentDefinition),
		builtIns: make(map[string]*agentsdk.AgentDefinition),
	}
	r.registerBuiltIns()
	return r
}

func (r *AgentRegistry) registerBuiltIns() {
	r.builtIns["general-purpose"] = &agentsdk.AgentDefinition{
		Name:        "general-purpose",
		Mode:        agentsdk.AgentModeGeneralPurpose,
		Description: "All-purpose agent with full tool access",
		Tools:       []string{"*"},
		Model:       "inherit",
	}

	r.builtIns["explore"] = &agentsdk.AgentDefinition{
		Name:         "explore",
		Mode:         agentsdk.AgentModeExplore,
		Description:  "Fast read-only exploration agent",
		Tools:        []string{"read_file", "grep", "glob", "list_dir", "shell"},
		Model:        "haiku",
		OmitCLAUDEMd: true,
	}

	r.builtIns["plan"] = &agentsdk.AgentDefinition{
		Name:         "plan",
		Mode:         agentsdk.AgentModePlan,
		Description:  "Planning agent with read-only tools",
		Tools:        []string{"read_file", "grep", "glob", "list_dir"},
		Model:        "inherit",
		OmitCLAUDEMd: true,
	}
}

// Get returns an agent definition by name. Checks custom agents first,
// then built-ins.
func (r *AgentRegistry) Get(name string) (*agentsdk.AgentDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if def, ok := r.agents[name]; ok {
		return def, true
	}
	def, ok := r.builtIns[name]
	return def, ok
}

// Register adds a custom agent definition.
func (r *AgentRegistry) Register(def *agentsdk.AgentDefinition) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if def.Name == "" {
		return fmt.Errorf("agent definition name is required")
	}

	r.agents[def.Name] = def
	return nil
}

// Names returns all registered agent names (custom + built-in).
func (r *AgentRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.agents)+len(r.builtIns))
	for name := range r.agents {
		names = append(names, name)
	}
	for name := range r.builtIns {
		if _, ok := r.agents[name]; !ok {
			names = append(names, name)
		}
	}
	return names
}
