package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/session"
)

// CompactionStrategy, ContextBudget, and CompactResult are defined in
// pkg/agentsdk/ and re-exported via sdk_aliases.go.

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

// SetThresholds overrides the compaction trigger and hard block ratios.
// Values must be between 0 and 1; invalid values are silently ignored.
func (cm *ContextManager) SetThresholds(compactTrigger, hardBlock float64) {
	if compactTrigger > 0 && compactTrigger <= 1 {
		cm.compactTrigger = compactTrigger
	}
	if hardBlock > 0 && hardBlock <= 1 {
		cm.hardBlock = hardBlock
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
// the configured trigger ratio (default 95%) to leave headroom for quality
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

// MeasureUsage populates the budget's component-level token counts based
// on the current conversation state. Call before each LLM request.
// systemPrompt is the full assembled prompt (including skill fragments);
// skillPrompts is the raw skill text that is already embedded in systemPrompt.
// We subtract skill tokens from the system prompt total to avoid double-counting.
func (cm *ContextManager) MeasureUsage(conv *Conversation, systemPrompt, skillPrompts string, toolDefs []provider.ToolDef) {
	skillTokens := 0
	if skillPrompts != "" {
		skillTokens = len(skillPrompts)/4 + 10
	}
	cm.budget.SkillPrompts = skillTokens
	cm.budget.SystemPrompt = len(systemPrompt)/4 + 10 - skillTokens

	toolTokens := 0
	for _, td := range toolDefs {
		toolTokens += len(td.Name)/4 + len(td.Description)/4 + len(td.InputSchema)/4 + 30
	}
	cm.budget.ToolDescriptions = toolTokens

	cm.budget.Conversation = estimateMessageTokens(conv.messages)
}

// Budget returns a copy of the current budget for external inspection.
func (cm *ContextManager) Budget() ContextBudget {
	return cm.budget
}

// ForceCompact runs the compaction strategy chain unconditionally,
// regardless of whether the trigger threshold has been reached.
func (cm *ContextManager) ForceCompact(ctx context.Context, conv *Conversation) CompactResult {
	result := CompactResult{
		BeforeTokens:   cm.EstimateTokens(conv),
		BeforeMsgCount: len(conv.messages),
	}

	if len(conv.messages) == 0 {
		result.AfterTokens = cm.EstimateTokens(conv)
		result.AfterMsgCount = 0
		return result
	}

	systemTokens := len(conv.SystemPrompt())/4 + 10
	messageBudget := cm.budget.EffectiveWindow() - systemTokens
	if messageBudget < 0 {
		messageBudget = 0
	}

	signals := ComputeConversationSignals(conv.messages)
	for _, s := range cm.strategies {
		if sa, ok := s.(SignalAware); ok {
			sa.SetSignals(signals)
		}
	}

	for _, s := range cm.strategies {
		tokensBefore := estimateMessageTokens(conv.messages)
		countBefore := len(conv.messages)
		msgs, err := s.Compact(ctx, conv.messages, messageBudget)
		if err != nil {
			continue
		}
		tokensAfter := estimateMessageTokens(msgs)
		countAfter := len(msgs)
		if tokensAfter < tokensBefore || countAfter < countBefore {
			result.StrategiesRun = append(result.StrategiesRun, s.Name())
		}
		conv.messages = msgs
	}

	result.AfterTokens = cm.EstimateTokens(conv)
	result.AfterMsgCount = len(conv.messages)
	return result
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

// VerdictContextBlock formats recent tool verdicts for agent awareness.
// Format (example):
//
//	Recent tool execution outcomes:
//	- shell: 42 total, 95% success rate
//	- file: 18 total, 100% success rate
//
// Returns an empty string if history is nil or empty (no context to add).
func VerdictContextBlock(verdictHist *session.VerdictHistory) string {
	if verdictHist == nil {
		return ""
	}

	summary := verdictHist.SummaryByTool()
	if len(summary) == 0 {
		return ""
	}

	var buf strings.Builder
	buf.WriteString("Recent tool execution outcomes:\n")
	for tool, stats := range summary {
		successRate := 0.0
		if stats.Total > 0 {
			successRate = float64(stats.Successful) / float64(stats.Total) * 100
		}
		fmt.Fprintf(&buf, "- %s: %d total, %.0f%% success rate\n",
			tool, stats.Total, successRate)
	}

	return buf.String()
}
