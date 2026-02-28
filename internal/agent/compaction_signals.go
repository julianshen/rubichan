package agent

import "github.com/julianshen/rubichan/internal/provider"

// ConversationSignals captures conversation-level metrics used to
// dynamically adjust compaction strategy behavior.
type ConversationSignals struct {
	ErrorDensity    float64 // fraction of messages containing error tool results (0.0-1.0)
	ToolCallDensity float64 // fraction of messages containing tool_use or tool_result (0.0-1.0)
	MessageCount    int
}

// SignalAware is an opt-in interface for compaction strategies that adjust
// their behavior based on conversation signals. ContextManager injects
// signals into strategies implementing this interface before each compaction.
type SignalAware interface {
	SetSignals(signals ConversationSignals)
}

// ComputeConversationSignals analyzes messages and returns conversation-level metrics.
func ComputeConversationSignals(messages []provider.Message) ConversationSignals {
	if len(messages) == 0 {
		return ConversationSignals{}
	}

	var errorCount, toolCount int
	for _, msg := range messages {
		hasError := false
		hasTool := false
		for _, block := range msg.Content {
			if block.Type == "tool_result" && block.IsError {
				hasError = true
			}
			if block.Type == "tool_use" || block.Type == "tool_result" {
				hasTool = true
			}
		}
		if hasError {
			errorCount++
		}
		if hasTool {
			toolCount++
		}
	}

	n := float64(len(messages))
	return ConversationSignals{
		ErrorDensity:    float64(errorCount) / n,
		ToolCallDensity: float64(toolCount) / n,
		MessageCount:    len(messages),
	}
}
