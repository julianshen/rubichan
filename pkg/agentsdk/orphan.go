package agentsdk

import "fmt"

// Reason strings for SealOrphanedToolUses, embedded in the synthesized
// tool_result content so the model — and anyone reading a captured
// conversation — can tell why each orphan was sealed.
const (
	// OrphanReasonToolCancel marks tools that never ran because the batch
	// was cancelled part-way.
	OrphanReasonToolCancel = "cancelled during tool execution"

	// OrphanReasonPanic marks tools left unanswered because the turn
	// goroutine panicked after the assistant message was committed.
	OrphanReasonPanic = "agent panic"
)

// ToolResultConversation is the slice of conversation behaviour orphan
// sealing needs. Both this package's Conversation and internal/agent's
// satisfy it, which is what lets the two agent loops share one sealing
// implementation instead of maintaining a copy each.
type ToolResultConversation interface {
	Messages() []Message
	AddToolResult(toolUseID, content string, isError bool)
}

// SealOrphanedToolUses walks conv and, for every tool_use block in the most
// recent assistant message that has no matching tool_result in a subsequent
// message, appends an error tool_result. It returns the number of orphans
// sealed.
//
// This exists because the Anthropic/OpenAI wire protocol requires every
// tool_use block to be followed by a tool_result. If a stream dies between
// tool_use emission and tool execution — or execution is cancelled after the
// assistant message has been committed — the next API call fails with a 400
// protocol error. Sealing orphans with a synthetic error result keeps the
// conversation valid for retry and for resume from a persisted snapshot.
//
// reason is embedded in the synthesized content so the model, and anyone
// reading a captured conversation, can tell why each orphan was sealed.
//
// Callers must invoke this on every exit path that follows a stream which may
// have emitted tool_use blocks: provider stream error, mid-stream error event,
// tool execution cancel, and panic recovery.
func SealOrphanedToolUses(conv ToolResultConversation, reason string) int {
	msgs := conv.Messages()
	if len(msgs) == 0 {
		return 0
	}

	// Find the last assistant message.
	assistantIdx := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			assistantIdx = i
			break
		}
	}
	if assistantIdx == -1 {
		return 0
	}

	// Collect tool_use IDs and names in that assistant message.
	type pendingToolUse struct {
		id, name string
	}
	var pending []pendingToolUse
	for _, block := range msgs[assistantIdx].Content {
		if block.Type == "tool_use" && block.ID != "" {
			pending = append(pending, pendingToolUse{id: block.ID, name: block.Name})
		}
	}
	if len(pending) == 0 {
		return 0
	}

	// Collect tool_result IDs that appear after the assistant message.
	answered := map[string]bool{}
	for i := assistantIdx + 1; i < len(msgs); i++ {
		for _, block := range msgs[i].Content {
			if block.Type == "tool_result" && block.ToolUseID != "" {
				answered[block.ToolUseID] = true
			}
		}
	}

	sealed := 0
	for _, p := range pending {
		if answered[p.id] {
			continue
		}
		toolName := p.name
		if toolName == "" {
			toolName = "<unknown>"
		}
		conv.AddToolResult(p.id,
			fmt.Sprintf("tool %s did not complete: %s", toolName, reason),
			true,
		)
		sealed++
	}
	return sealed
}
