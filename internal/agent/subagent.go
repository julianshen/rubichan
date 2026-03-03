package agent

import "context"

// SubagentConfig defines how a child agent is created.
type SubagentConfig struct {
	Name         string   // Identifier (e.g., "explorer")
	SystemPrompt string   // Additional system prompt (appended to base)
	Tools        []string // Whitelist of tool names (nil = all parent tools)
	MaxTurns     int      // Turn limit (0 = default 10)
	MaxTokens    int      // Output token budget (0 = inherit)
	Model        string   // Override model (empty = inherit parent)
	Depth        int      // Current nesting level (0 = top-level)
	MaxDepth     int      // Maximum nesting (0 = default 3)
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

// SubagentSpawner creates and runs child agents.
type SubagentSpawner interface {
	Spawn(ctx context.Context, cfg SubagentConfig, prompt string) (*SubagentResult, error)
}

const (
	defaultSubagentMaxTurns = 10
	defaultSubagentMaxDepth = 3
)
