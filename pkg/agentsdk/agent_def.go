package agentsdk

// AgentMode is a built-in agent type.
type AgentMode string

const (
	// AgentModeGeneralPurpose has all tools, inherits parent model.
	AgentModeGeneralPurpose AgentMode = "general-purpose"
	// AgentModeExplore is read-only + shell, uses fast model.
	AgentModeExplore AgentMode = "explore"
	// AgentModePlan is read-only planning, no CLAUDE.md.
	AgentModePlan AgentMode = "plan"
	// AgentModeVerification is background checking agent.
	AgentModeVerification AgentMode = "verification"
)

// AgentDefinition describes an agent's capabilities and constraints.
type AgentDefinition struct {
	// Name is the agent identifier (e.g., "explore", "custom-docs").
	Name string
	// Mode is the built-in type. Empty for custom agents.
	Mode AgentMode
	// Description explains the agent's purpose.
	Description string
	// Tools is the tool filter. ["*"] means all tools.
	Tools []string
	// DisallowedTools removes tools from the wildcard set.
	DisallowedTools []string
	// Model overrides the parent model. "inherit" uses parent's model.
	Model string
	// SystemPrompt is prepended to the conversation.
	SystemPrompt string
	// OmitCLAUDEMd excludes CLAUDE.md from context.
	OmitCLAUDEMd bool
	// MaxTurns caps the agent's execution. 0 means inherit.
	MaxTurns int
}

// IsCoordinator returns true if this agent only runs coordinator tools
// (subagent spawn, task stop, message send).
func (d *AgentDefinition) IsCoordinator() bool {
	if len(d.Tools) != 3 {
		return false
	}
	coordTools := map[string]bool{"Agent": true, "TaskStop": true, "SendMessage": true}
	for _, t := range d.Tools {
		if !coordTools[t] {
			return false
		}
	}
	return true
}
