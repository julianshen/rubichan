package agent

import "github.com/julianshen/rubichan/pkg/agentsdk"

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

// synthesizeMissingToolResults seals unanswered tool_use blocks in the most
// recent assistant message. Returns the number of orphans sealed.
//
// The walk itself lives in pkg/agentsdk so this loop and the portable one
// share a single implementation — a fix to either reaches both. See
// agentsdk.SealOrphanedToolUses for why the protocol requires this.
func synthesizeMissingToolResults(conv *Conversation, reason string) int {
	return agentsdk.SealOrphanedToolUses(conv, reason)
}
