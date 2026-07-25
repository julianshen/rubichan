package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// assembledTurn is what runLoop needs from a completed stream: the assistant
// content blocks (already committed to the conversation and persisted), the
// tool calls awaiting execution, and the exit reason a clean terminal path
// should report for this turn.
type assembledTurn struct {
	blocks       []provider.ContentBlock
	pendingTools []provider.ToolUseBlock
	exitReason   agentsdk.TurnExitReason
}

// assembleAssistantTurn turns a consumed stream into a committed assistant
// message: max-output-tokens recovery (escalation, then continuation
// prompts), truncated-tool cleanup, thinking-block prepending, stream-error
// terminalization, text-based tool extraction for non-native models, the
// empty-response placeholder, the after-response hook, and finally the
// conversation commit. Extracted from runLoop so the loop reads as
// orchestration; behavior is unchanged.
//
// The result is meaningful only for stepProceed. stepRetryTurn means a
// recovery mutated loop state and the turn must re-enter without being
// consumed; stepEnded means terminal events were already emitted.
func (a *Agent) assembleAssistantTurn(ctx context.Context, ch chan<- TurnEvent, ls *loopState, acc *agentsdk.StreamAccumulator, execStream *streamingToolExecutor, thinkingBuf, stopReason string, useNativeTools bool, totalInputTokens, totalOutputTokens int) (assembledTurn, loopStepOutcome) {
	// Handle max_tokens truncation: inject a continuation message and
	// retry so the model can complete its response, up to
	// maxOutputTokensRecoveryLimit times. Only retry when there are no
	// pending tool calls; otherwise the truncated-tool detection below
	// handles it.
	if stopReason == agentsdk.StopReasonMaxTokens {
		hasPendingTools := len(acc.PendingTools()) > 0 || acc.HasPartialTool()
		if hasPendingTools {
			a.logger.Warn("response hit output token limit with %d pending tool calls; tool arguments may be truncated", len(acc.PendingTools()))
		} else if ls.maxOutputTokens < escalatedMaxOutputTokens {
			a.logger.Warn("output token limit hit; escalating max_tokens from %d to %d", ls.maxOutputTokens, escalatedMaxOutputTokens)
			a.escalateMaxTokens(ls)
			a.emit(ctx, ch, TurnEvent{Type: "max_tokens_escalation"})
			ls.turnCount--
			ls.lastContinueReason = ContinueMaxTokensRecovery
			return assembledTurn{}, stepRetryTurn
		} else if ls.maxTokensRecoveryAttempts < maxOutputTokensRecoveryLimit {
			ls.maxTokensRecoveryAttempts++
			// Finalize buffered text so the truncated partial response is
			// retained in history — the continuation prompt is meaningless
			// if the model can't see what it already wrote. No partial tool
			// can exist here: this branch requires hasPendingTools == false.
			acc.Finish()
			partialBlocks := acc.Blocks()
			a.conversation.AddAssistant(partialBlocks)
			a.persistMessage("assistant", partialBlocks)
			a.conversation.AddUser(fmt.Sprintf(
				"[max_output_tokens recovery %d/%d] Continue your response from where you left off.",
				ls.maxTokensRecoveryAttempts, maxOutputTokensRecoveryLimit))
			a.emit(ctx, ch, TurnEvent{Type: "max_tokens_recovery"})
			a.saveSnapshotIfNeeded()
			ls.turnCount--
			ls.lastContinueReason = ContinueMaxTokensRecovery
			return assembledTurn{}, stepRetryTurn
		} else {
			a.logger.Warn("response truncated by output token limit after %d recovery attempts", ls.maxTokensRecoveryAttempts)
		}
	}

	// Capture accumulated text before finalizing, for text-based tool extraction.
	accumulatedText := acc.CurrentText()

	// Detect truncated tool calls: if the stream ended with a partially
	// accumulated tool whose input JSON is invalid, discard it and warn.
	if acc.DropInvalidPartialTool() {
		a.emit(ctx, ch, TurnEvent{Type: "text_delta", Text: "\n⚠️ Tool call truncated by output limit.\n"})
	}

	// Finalize any remaining text or tool.
	acc.Finish()
	blocks := acc.Blocks()
	pendingTools := acc.PendingTools()

	// Prepend thinking block if the model produced extended thinking output.
	if thinkingBuf != "" {
		blocks = append([]provider.ContentBlock{{
			Type: agentsdk.BlockTypeThinking,
			Text: thinkingBuf,
		}}, blocks...)
	}

	// On stream error, discard partial blocks to prevent conversation
	// corruption but surface any completed streaming dispatches to the
	// event channel. executeSingleTool emits tool_progress events as
	// the tool runs; if we don't also emit a matching tool_call +
	// tool_result, the UI sees orphan progress with no terminal event.
	if ls.streamErr {
		if unmatched := surfaceStreamedResults(ctx, a, ch, pendingTools, execStream.Drain()); unmatched > 0 {
			// Invariant broken: every dispatched tool should have been
			// appended to pendingTools before Dispatch ran. If this
			// fires, a future refactor reordered the Dispatch site.
			a.logger.Warn("streamErr: %d drained tool result(s) had IDs not in pendingTools; tool_call events were skipped", unmatched)
		}
		// Defensive: currently a no-op because AddAssistant has not been called
		// yet on this path, but protects against future reorderings.
		synthesizeMissingToolResults(a.conversation, orphanReasonStreamError)
		a.emit(ctx, ch, a.makeDoneEvent(totalInputTokens, totalOutputTokens, agentsdk.ExitProviderError))
		return assembledTurn{}, stepEnded
	}

	// For non-native models, parse <tool_use> XML blocks from the text response
	// and inject them into pendingTools so the normal execution path handles them.
	if !useNativeTools && len(pendingTools) == 0 && accumulatedText != "" {
		textCalls := tools.ParseTextToolCalls(accumulatedText)
		if len(textCalls) == 0 && strings.Contains(accumulatedText, "<tool_use>") {
			a.logger.Warn("model attempted tool call in text but XML parsing found no valid blocks")
		}
		if len(textCalls) > 0 {
			// Strip <tool_use> XML from the text block so the model
			// doesn't see its own XML format echoed back on the next turn.
			for i := range blocks {
				if blocks[i].Type == "text" {
					blocks[i].Text = strings.TrimSpace(tools.StripToolUseXML(blocks[i].Text))
				}
			}
		}
		for _, tc := range textCalls {
			pendingTools = append(pendingTools, tc)
			blocks = append(blocks, provider.ContentBlock{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Name,
				Input: tc.Input,
			})
		}
	}

	// If the LLM returned no content at all (no text, no tool calls),
	// emit an error and add a placeholder assistant message to keep the
	// conversation valid (every user message must be followed by an
	// assistant message). The placeholder downgrades the exit reason
	// from ExitCompleted to ExitEmptyResponse so observability can
	// distinguish "model said nothing" from "model finished normally".
	exitReason := agentsdk.ExitCompleted
	if len(blocks) == 0 && len(pendingTools) == 0 {
		blocks = append(blocks, provider.ContentBlock{Type: "text", Text: emptyModelResponseText})
		a.emit(ctx, ch, TurnEvent{Type: "error", Error: fmt.Errorf("empty response from model")})
		exitReason = agentsdk.ExitEmptyResponse
	}

	// On final-response turns, give HookOnAfterResponse handlers a chance
	// to rewrite the assistant text before it's persisted. The streamed
	// text already reached the user, but the persisted version is what
	// subsequent turns see in conversation context — that's the surface
	// transform skills target. A turn is final on either no-pending-tools
	// (clean completion) or when task_complete is in the pending batch.
	finalReason, isFinal := terminalExitReason(pendingTools, exitReason)
	if isFinal {
		blocks = a.applyAfterResponseHook(ctx, blocks, finalReason)
	}

	// Add assistant message with accumulated blocks
	if len(blocks) > 0 {
		a.conversation.AddAssistant(blocks)
		a.persistMessage("assistant", blocks)
	}

	return assembledTurn{blocks: blocks, pendingTools: pendingTools, exitReason: exitReason}, stepProceed
}
