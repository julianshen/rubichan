package agent

import "fmt"

// Reason strings passed to synthesizeMissingToolResults. Embedded in
// the synthesized tool_result content so the model (and anyone
// reading a captured conversation) can distinguish why each orphan
// was sealed.
const (
	orphanReasonStreamError = "stream error"
	orphanReasonToolCancel  = "cancelled during tool execution"
	orphanReasonPanic       = "agent panic"
	orphanReasonLoad        = "loaded from persisted session"
)

// emptyModelResponseText is the placeholder inserted when the model
// returns no blocks and no tool calls. Keeping it as a single source
// of truth makes the ExitEmptyResponse classification unambiguous.
const emptyModelResponseText = "[empty response from model]"

// synthesizeMissingToolResults walks the conversation and, for every
// tool_use block in the most recent assistant message that does not have
// a matching tool_result in a subsequent message, appends an error
// tool_result. Returns the number of orphans sealed.
//
// This exists because the Anthropic/OpenAI wire protocol requires every
// tool_use block to be followed by a tool_result. If the stream dies
// between tool_use emission and tool execution — or if execution is
// cancelled after the assistant message has been committed — the next
// API call fails with a 400 protocol error. Sealing orphans with a
// synthetic error result keeps the conversation valid for retry and
// for resume from a persisted snapshot.
//
// Called from every agent-loop exit path that follows a stream which
// may have emitted tool_use blocks: provider stream error, mid-stream
// error event, tool execution cancel, and the deferred panic handler.
func synthesizeMissingToolResults(conv *Conversation, reason string) int {
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
