package agentsdk

import "context"

// CompactionStrategy defines a strategy for reducing conversation size.
// Strategies are run in order from lightest to heaviest; the chain stops
// once the conversation fits within the token budget.
type CompactionStrategy interface {
	Name() string
	Compact(ctx context.Context, messages []Message, budget int) ([]Message, error)
}

// ContextBudget tracks token usage by component category.
type ContextBudget struct {
	Total           int // configured max (from config.Agent.ContextBudget)
	MaxOutputTokens int // reserved for LLM response (e.g., 4096)

	// Measured usage (updated before each LLM call):
	SystemPrompt     int // base prompt + AGENT.md + memories
	SkillPrompts     int // active skill prompt fragments
	ToolDescriptions int // tool defs sent to LLM (grows with MCP/skills)
	Conversation     int // messages + tool results
}

// EffectiveWindow returns the usable context after reserving output tokens.
func (b *ContextBudget) EffectiveWindow() int {
	ew := b.Total - b.MaxOutputTokens
	if ew < 0 {
		return 0
	}
	return ew
}

// UsedTokens returns total tokens consumed across all components.
func (b *ContextBudget) UsedTokens() int {
	return b.SystemPrompt + b.SkillPrompts + b.ToolDescriptions + b.Conversation
}

// RemainingTokens returns how many tokens are available for conversation growth.
func (b *ContextBudget) RemainingTokens() int {
	return b.EffectiveWindow() - b.UsedTokens()
}

// UsedPercentage returns the fraction of the effective window in use (0.0-1.0+).
func (b *ContextBudget) UsedPercentage() float64 {
	ew := b.EffectiveWindow()
	if ew <= 0 {
		return 1.0
	}
	return float64(b.UsedTokens()) / float64(ew)
}

// CompactResult reports what happened during a compaction.
type CompactResult struct {
	BeforeTokens   int
	AfterTokens    int
	BeforeMsgCount int
	AfterMsgCount  int
	StrategiesRun  []string
}
