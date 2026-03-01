package agent

import (
	"context"

	"github.com/julianshen/rubichan/internal/provider"
)

// CompactionStrategy defines a strategy for reducing conversation size.
// Strategies are run in order from lightest to heaviest; the chain stops
// once the conversation fits within the token budget.
type CompactionStrategy interface {
	Name() string
	Compact(ctx context.Context, messages []provider.Message, budget int) ([]provider.Message, error)
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

// ContextManager tracks token usage and compacts conversation history
// to stay within a configured budget using a chain of strategies.
type ContextManager struct {
	budget         ContextBudget
	compactTrigger float64 // fraction of effective window to trigger compaction (default 0.95)
	hardBlock      float64 // fraction of effective window to block new messages (default 0.98)
	strategies     []CompactionStrategy
}

// NewContextManager creates a new ContextManager with the given total budget
// and max output tokens. The effective window is total - maxOutputTokens.
// Pass maxOutputTokens=0 to use the full budget as the effective window.
func NewContextManager(totalBudget, maxOutputTokens int) *ContextManager {
	return &ContextManager{
		budget: ContextBudget{
			Total:           totalBudget,
			MaxOutputTokens: maxOutputTokens,
		},
		compactTrigger: 0.95,
		hardBlock:      0.98,
		strategies: []CompactionStrategy{
			NewToolResultClearingStrategy(),
			&truncateStrategy{},
		},
	}
}

// SetStrategies replaces the compaction strategy chain. An empty or nil
// slice restores the default chain (tool clearing + truncation).
func (cm *ContextManager) SetStrategies(strategies []CompactionStrategy) {
	if len(strategies) == 0 {
		cm.strategies = []CompactionStrategy{
			NewToolResultClearingStrategy(),
			&truncateStrategy{},
		}
		return
	}
	cm.strategies = strategies
}

// ShouldCompact returns true when the estimated token count exceeds the
// proactive trigger threshold (default 95% of effective window), allowing
// compaction to start before the conversation fully exhausts the context window.
func (cm *ContextManager) ShouldCompact(conv *Conversation) bool {
	threshold := int(float64(cm.budget.EffectiveWindow()) * cm.compactTrigger)
	return cm.EstimateTokens(conv) > threshold
}

// IsBlocked returns true when the conversation has exceeded the hard block
// threshold (default 98% of effective window), indicating new messages should
// not be added.
func (cm *ContextManager) IsBlocked(conv *Conversation) bool {
	threshold := int(float64(cm.budget.EffectiveWindow()) * cm.hardBlock)
	return cm.EstimateTokens(conv) > threshold
}

// Compact runs the compaction strategy chain until the conversation fits
// within the token budget. Strategies are tried in order; the chain stops
// as soon as the conversation is under budget. Triggers proactively at
// the configured triggerRatio (default 70%) to leave headroom for quality
// summarization.
func (cm *ContextManager) Compact(ctx context.Context, conv *Conversation) {
	if !cm.ShouldCompact(conv) {
		return
	}
	// Subtract system prompt overhead so strategies only need to fit messages.
	systemTokens := len(conv.SystemPrompt())/4 + 10
	messageBudget := cm.budget.EffectiveWindow() - systemTokens
	if messageBudget < 0 {
		messageBudget = 0
	}
	// Compute signals once; inject into strategies that support dynamic adjustment.
	signals := ComputeConversationSignals(conv.messages)
	for _, s := range cm.strategies {
		if sa, ok := s.(SignalAware); ok {
			sa.SetSignals(signals)
		}
	}

	for i, s := range cm.strategies {
		// First strategy always runs (we passed ShouldCompact).
		// Subsequent strategies only run if still over 100% budget.
		if i > 0 && !cm.ExceedsBudget(conv) {
			return
		}
		result, err := s.Compact(ctx, conv.messages, messageBudget)
		if err != nil {
			continue
		}
		conv.messages = result
	}
}

// EstimateTokens estimates the token count for a conversation using
// a ~4 chars per token heuristic with +10 overhead per content block.
func (cm *ContextManager) EstimateTokens(conv *Conversation) int {
	total := len(conv.SystemPrompt())/4 + 10
	total += estimateMessageTokens(conv.messages)
	return total
}

// estimateMessageTokens estimates the token count for a slice of messages
// using a ~4 chars per token heuristic with +10 overhead per content block.
func estimateMessageTokens(msgs []provider.Message) int {
	total := 0
	for _, msg := range msgs {
		for _, block := range msg.Content {
			chars := len(block.Text) + len(block.ID) + len(block.Name) +
				len(block.ToolUseID) + len(block.Input)
			total += chars/4 + 10
		}
	}
	return total
}

// ExceedsBudget returns true if the estimated token count exceeds the effective window.
func (cm *ContextManager) ExceedsBudget(conv *Conversation) bool {
	return cm.EstimateTokens(conv) > cm.budget.EffectiveWindow()
}

// Truncate removes the oldest messages until the conversation is within budget.
// Deprecated: use Compact() which runs the full strategy chain.
func (cm *ContextManager) Truncate(conv *Conversation) {
	cm.Compact(context.Background(), conv)
}

// truncateStrategy is the last-resort compaction strategy that removes
// the oldest message pairs to fit within the token budget.
type truncateStrategy struct{}

func (s *truncateStrategy) Name() string { return "truncate" }

func (s *truncateStrategy) Compact(_ context.Context, messages []provider.Message, budget int) ([]provider.Message, error) {
	for estimateMessageTokens(messages) > budget && len(messages) > 2 {
		start := 0
		for start < len(messages) && hasToolResult(messages[start]) {
			start++
		}

		remove := start + 2
		if remove > len(messages)-2 {
			remove = len(messages) - 2
		}
		if remove <= 0 {
			break
		}
		messages = messages[remove:]
	}
	return messages, nil
}

// hasToolResult returns true if any content block in the message is a tool_result.
func hasToolResult(msg provider.Message) bool {
	for _, block := range msg.Content {
		if block.Type == "tool_result" {
			return true
		}
	}
	return false
}

// hasToolUse returns true if any content block in the message is a tool_use.
func hasToolUse(msg provider.Message) bool {
	for _, block := range msg.Content {
		if block.Type == "tool_use" {
			return true
		}
	}
	return false
}
