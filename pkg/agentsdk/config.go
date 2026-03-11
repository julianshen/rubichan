package agentsdk

// AgentConfig holds agent-relevant parameters for SDK consumers.
// Unlike internal/config.Config, this has no TOML paths, no API keys,
// and no CLI-specific settings — just the knobs external applications need.
type AgentConfig struct {
	Model                  string  // LLM model identifier (e.g., "claude-sonnet-4-5")
	MaxTurns               int     // maximum turns before stopping (default 50)
	ContextBudget          int     // total context window tokens (default 100000)
	MaxOutputTokens        int     // reserved for LLM response (default 4096)
	CompactTrigger         float64 // fraction of window to trigger compaction (default 0.95)
	HardBlock              float64 // fraction of window to block new messages (default 0.98)
	ResultOffloadThreshold int     // tool result byte limit before offloading (default 4096)
	ToolDeferralThreshold  float64 // tool deferral budget ratio (default 0.10)
	SystemPrompt           string  // optional system prompt override
}

// DefaultAgentConfig returns an AgentConfig populated with sensible defaults.
func DefaultAgentConfig() AgentConfig {
	return AgentConfig{
		Model:                  "claude-sonnet-4-5",
		MaxTurns:               50,
		ContextBudget:          100000,
		MaxOutputTokens:        4096,
		CompactTrigger:         0.95,
		HardBlock:              0.98,
		ResultOffloadThreshold: 4096,
		ToolDeferralThreshold:  0.10,
	}
}
