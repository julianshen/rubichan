package agentsdk

// SubagentMode controls how the parent waits for the child.
type SubagentMode string

const (
	// SubagentModeSync blocks until the child completes.
	SubagentModeSync SubagentMode = "sync"
	// SubagentModeAsync returns immediately with a progress channel.
	SubagentModeAsync SubagentMode = "async"
	// SubagentModeFork runs in process isolation.
	SubagentModeFork SubagentMode = "fork"
)

// SubagentConfig describes a child agent to spawn.
type SubagentConfig struct {
	// Name is the agent identifier (e.g., "explorer").
	Name string
	// SystemPrompt is additional system prompt (appended to base).
	SystemPrompt string
	// Tools is a whitelist of tool names (nil = all parent tools).
	Tools []string
	// MaxTurns is the turn limit (0 = default 10).
	MaxTurns int
	// MaxTokens is the output token budget (0 = inherit).
	MaxTokens int
	// ContextBudget is the context window override (0 = inherit parent).
	ContextBudget int
	// Model overrides the parent model (empty = inherit parent).
	Model string
	// Depth is the current nesting level (0 = top-level).
	Depth int
	// MaxDepth is the maximum nesting (0 = default 3).
	MaxDepth int
	// InheritSkills: nil/default = inherit currently active parent skills.
	InheritSkills *bool
	// ExtraSkills are additional skills to include.
	ExtraSkills []string
	// DisableSkills are skills to exclude.
	DisableSkills []string
	// Isolation: "", "worktree" — if "worktree", spawn in isolated worktree.
	Isolation string
}

// SubagentResult is returned when a child agent completes.
type SubagentResult struct {
	Name         string   // Which agent definition was used
	Output       string   // Final text output from the child
	ToolsUsed    []string // Tools the child called
	TurnCount    int      // How many turns the child took
	InputTokens  int      // Total input tokens consumed
	OutputTokens int      // Total output tokens consumed
	Error        error    // Non-nil if the child failed
}

// SubagentStatus tracks child execution state.
type SubagentStatus struct {
	ID        string
	AgentName string
	Mode      SubagentMode
	Done      bool
	Error     error
	Result    string
}
