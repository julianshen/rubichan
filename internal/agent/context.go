package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/session"
)

// CompactionStrategy, ContextBudget, and CompactResult are defined in
// pkg/agentsdk/ and re-exported via sdk_aliases.go.

// ErrCompactionExhausted is returned from Compact when the strategy
// chain has failed MaxConsecutiveCompactionFailures times in a row
// without reducing token count. The loop must terminate rather than
// retry — infinite compact-fail-retry burns API budget with no
// progress. Observed in Claude Code's query.ts as the
// "250K API calls/day" incident that motivated its circuit breaker.
var ErrCompactionExhausted = errors.New("compaction failed repeatedly; circuit breaker tripped")

// MaxConsecutiveCompactionFailures is the threshold for the circuit
// breaker. Matches Claude Code's MAX_CONSECUTIVE_AUTOCOMPACT_FAILURES=3.
const MaxConsecutiveCompactionFailures = 3

// ContextManager tracks token usage and compacts conversation history
// to stay within a configured budget using a chain of strategies.
type ContextManager struct {
	mu                  sync.RWMutex // protects budget and thresholds
	budget              ContextBudget
	compactTrigger      float64 // fraction of effective window to trigger compaction (default 0.95)
	hardBlock           float64 // fraction of effective window to block new messages (default 0.98)
	warnThreshold       float64 // fraction for WarningLow (default 0.70)
	cautionThreshold    float64 // fraction for WarningMedium (default 0.80)
	strategies          []CompactionStrategy
	consecutiveFailures int // circuit breaker counter for repeated no-shrink Compact calls
	collapseStore       *CollapseStore
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
		compactTrigger:   0.95,
		hardBlock:        0.98,
		warnThreshold:    0.70,
		cautionThreshold: 0.80,
		strategies: []CompactionStrategy{
			NewToolResultClearingStrategy(),
			&truncateStrategy{},
		},
	}
}

// SetThresholds overrides warning and compaction ratios.
// Values must be between 0 and 1 and in ascending order:
// warnThreshold <= cautionThreshold <= compactTrigger <= hardBlock.
// Invalid or out-of-order values are silently ignored.
func (cm *ContextManager) SetThresholds(warnThreshold, cautionThreshold, compactTrigger, hardBlock float64) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if warnThreshold > 0 && warnThreshold <= 1 {
		cm.warnThreshold = warnThreshold
	}
	if cautionThreshold > 0 && cautionThreshold <= 1 {
		cm.cautionThreshold = cautionThreshold
	}
	if compactTrigger > 0 && compactTrigger <= 1 {
		cm.compactTrigger = compactTrigger
	}
	if hardBlock > 0 && hardBlock <= 1 {
		cm.hardBlock = hardBlock
	}
	// Enforce ascending order to prevent nonsensical level jumps.
	if cm.warnThreshold > cm.cautionThreshold {
		cm.warnThreshold, cm.cautionThreshold = cm.cautionThreshold, cm.warnThreshold
	}
	if cm.cautionThreshold > cm.compactTrigger {
		cm.cautionThreshold, cm.compactTrigger = cm.compactTrigger, cm.cautionThreshold
	}
	if cm.compactTrigger > cm.hardBlock {
		cm.compactTrigger, cm.hardBlock = cm.hardBlock, cm.compactTrigger
	}
}

// Thresholds returns the current warning ratios: warnThreshold, cautionThreshold,
// compactTrigger, and hardBlock.
func (cm *ContextManager) Thresholds() (warnThreshold, cautionThreshold, compactTrigger, hardBlock float64) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.warnThreshold, cm.cautionThreshold, cm.compactTrigger, cm.hardBlock
}

// BudgetWithThresholds returns a copy of the current budget and all threshold
// ratios under a single critical section, preventing torn reads.
func (cm *ContextManager) BudgetWithThresholds() (ContextBudget, float64, float64, float64, float64) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.budget, cm.warnThreshold, cm.cautionThreshold, cm.compactTrigger, cm.hardBlock
}

// SetStrategies replaces the compaction strategy chain. An empty or nil
// slice restores the default chain (tool clearing + truncation).
func (cm *ContextManager) SetStrategies(strategies []CompactionStrategy) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if len(strategies) == 0 {
		cm.strategies = []CompactionStrategy{
			NewToolResultClearingStrategy(),
			&truncateStrategy{},
		}
		return
	}
	cm.strategies = strategies
}

// SetCollapseStore attaches a collapse store for staged archival.
func (cm *ContextManager) SetCollapseStore(store *CollapseStore) {
	cm.mu.Lock()
	cm.collapseStore = store
	cm.mu.Unlock()
}

// ShouldCompact returns true when the estimated token count exceeds the
// proactive trigger threshold (default 95% of effective window), allowing
// compaction to start before the conversation fully exhausts the context window.
func (cm *ContextManager) ShouldCompact(conv *Conversation) bool {
	cm.mu.RLock()
	threshold := int(float64(cm.budget.EffectiveWindow()) * cm.compactTrigger)
	cm.mu.RUnlock()
	return cm.EstimateTokens(conv) > threshold
}

// IsBlocked returns true when the conversation has exceeded the hard block
// threshold (default 98% of effective window), indicating new messages should
// not be added.
func (cm *ContextManager) IsBlocked(conv *Conversation) bool {
	cm.mu.RLock()
	threshold := int(float64(cm.budget.EffectiveWindow()) * cm.hardBlock)
	cm.mu.RUnlock()
	return cm.EstimateTokens(conv) > threshold
}

// Compact runs the compaction strategy chain until the conversation fits
// within the token budget. Strategies are tried in order; the chain stops
// as soon as the conversation is under budget. Triggers proactively at
// the configured trigger ratio (default 95%) to leave headroom for quality
// summarization.
func (cm *ContextManager) Compact(ctx context.Context, conv *Conversation) error {
	if !cm.ShouldCompact(conv) {
		return nil
	}
	// Subtract system prompt overhead so strategies only need to fit messages.
	systemTokens := len(conv.SystemPrompt())/4 + 10
	cm.mu.RLock()
	messageBudget := cm.budget.EffectiveWindow() - systemTokens
	cm.mu.RUnlock()
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

	beforeTokens := estimateMessageTokens(conv.messages)
	anyStrategySucceeded := false

	for i, s := range cm.strategies {
		// First strategy always runs (we passed ShouldCompact).
		// Subsequent strategies only run if still over 100% budget.
		if i > 0 && !cm.ExceedsBudget(conv) {
			break
		}
		result, err := s.Compact(ctx, conv.messages, messageBudget)
		if err != nil {
			continue
		}
		conv.messages = result
		anyStrategySucceeded = true
	}

	// Apply collapse store projection after strategies run.
	if cm.collapseStore != nil && cm.collapseStore.IsEnabled() && cm.collapseStore.HasCommits() {
		conv.messages = cm.collapseStore.ProjectView(conv.messages)
	}

	afterTokens := estimateMessageTokens(conv.messages)
	shrank := afterTokens < beforeTokens

	// Real progress requires BOTH a non-erroring strategy AND an actual
	// token reduction. Silent no-op strategies must not reset the breaker.
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if anyStrategySucceeded && shrank {
		cm.consecutiveFailures = 0
		return nil
	}

	cm.consecutiveFailures++
	if cm.consecutiveFailures >= MaxConsecutiveCompactionFailures {
		return ErrCompactionExhausted
	}
	return nil
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

	toolTokens := 0
	for _, td := range toolDefs {
		toolTokens += len(td.Name)/4 + len(td.Description)/4 + len(td.InputSchema)/4 + 30
	}

	convTokens := estimateMessageTokens(conv.messages)

	cm.mu.Lock()
	cm.budget.SkillPrompts = skillTokens
	cm.budget.SystemPrompt = len(systemPrompt)/4 + 10 - skillTokens
	cm.budget.ToolDescriptions = toolTokens
	cm.budget.Conversation = convTokens
	cm.mu.Unlock()
}

// SetBudget updates the total token budget. This is used when the user
// specifies a custom budget via message directives (e.g., "+500k").
func (cm *ContextManager) SetBudget(total int) {
	cm.mu.Lock()
	cm.budget.Total = total
	cm.mu.Unlock()
}

// Budget returns a copy of the current budget for external inspection.
func (cm *ContextManager) Budget() ContextBudget {
	cm.mu.RLock()
	b := cm.budget
	cm.mu.RUnlock()
	return b
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
	cm.mu.RLock()
	messageBudget := cm.budget.EffectiveWindow() - systemTokens
	cm.mu.RUnlock()
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
		// If strategy supports Snip, capture the result for telemetry.
		// Use msgs (post-Compact) rather than conv.messages (pre-Compact)
		// to ensure SnipResult reflects the actual compacted state.
		if snipper, ok := s.(interface {
			Snip([]Message, int) SnipResult
		}); ok {
			snip := snipper.Snip(msgs, messageBudget)
			if snip.BoundaryMsg != nil {
				result.SnipResults = append(result.SnipResults, snip)
			}
		}
		conv.messages = msgs
	}

	// Apply collapse store projection.
	if cm.collapseStore != nil && cm.collapseStore.IsEnabled() && cm.collapseStore.HasCommits() {
		conv.messages = cm.collapseStore.ProjectView(conv.messages)
	}

	result.AfterTokens = cm.EstimateTokens(conv)
	result.AfterMsgCount = len(conv.messages)
	return result
}

// ExceedsBudget returns true if the estimated token count exceeds the effective window.
func (cm *ContextManager) ExceedsBudget(conv *Conversation) bool {
	cm.mu.RLock()
	ew := cm.budget.EffectiveWindow()
	cm.mu.RUnlock()
	return cm.EstimateTokens(conv) > ew
}

// Truncate removes the oldest messages until the conversation is within budget.
// Deprecated: use Compact() which runs the full strategy chain.
func (cm *ContextManager) Truncate(conv *Conversation) {
	_ = cm.Compact(context.Background(), conv)
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
