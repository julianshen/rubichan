package agent

import (
	"context"
	"fmt"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// runToolPhase is the tool half of one loop iteration, starting after the
// stop-hook gate: clean completion when nothing is pending, merging of
// results from tools already dispatched mid-stream, the token-budget gate
// (with its nudge), no-progress detection, the task_complete exit (siblings
// executed first), tool execution itself, wake-event draining, and the
// post-execution context re-measure with window advice. Extracted from
// runLoop so the loop reads as orchestration; behavior is unchanged.
//
// joinBackgroundTasks is the current iteration's join closure; terminal
// paths that executed tools invoke it before emitting done so per-turn
// background work is collected, exactly as the inline code did. stepEnded
// means a terminal path already emitted its events and runLoop must return;
// stepProceed means tools ran and the loop should continue to the next turn.
func (a *Agent) runToolPhase(ctx context.Context, ch chan<- TurnEvent, ls *loopState, asm assembledTurn, execStream *streamingToolExecutor, joinBackgroundTasks func(), systemPrompt, skillPromptText string, activeTools []provider.ToolDef, totalInputTokens, totalOutputTokens int) loopStepOutcome {
	blocks, pendingTools, exitReason := asm.blocks, asm.pendingTools, asm.exitReason

	// If no pending tool calls, we're done. Drain any background
	// dispatches so goroutines don't outlive the turn (should be
	// empty in this branch, but Drain is cheap and safe).
	if len(pendingTools) == 0 {
		_ = execStream.Drain()
		a.emit(ctx, ch, a.makeDoneEvent(totalInputTokens, totalOutputTokens, exitReason))
		return stepEnded
	}

	// Drain any tools dispatched during streaming; executeTools
	// will merge these results into its output by tool_use ID.
	var streamedResults map[string]toolExecResult
	if drained := execStream.Drain(); len(drained) > 0 {
		streamedResults = make(map[string]toolExecResult, len(drained))
		for _, r := range drained {
			streamedResults[r.toolUseID] = r
		}
	}

	// Check token budget before executing tools.
	// EffectiveWindow is used as the budget limit; this monitors output tokens
	// against the available context window and stops before overflow.
	ctxBudget := a.context.Budget()
	dec := CheckTokenBudget(ls.budgetTracker, "", ctxBudget.EffectiveWindow(), totalOutputTokens)
	if dec.Action == BudgetStop {
		reason := "completion threshold"
		if dec.CompletionEvent.DiminishingReturns {
			reason = "diminishing returns"
		}
		a.logger.Warn("token budget stop: %s (%d%%)", reason, dec.Pct)
		// A cancelled batch leaves trailing tool_use blocks unanswered; seal
		// them before exiting or the next provider call fails a protocol check.
		a.executeToolsSealingCancellation(ctx, ch, pendingTools, streamedResults)
		joinBackgroundTasks()
		a.emit(ctx, ch, TurnEvent{Type: "budget_stop"})
		exitReason := agentsdk.ExitBudgetExceeded
		if dec.CompletionEvent.DiminishingReturns {
			exitReason = agentsdk.ExitDiminishingReturns
		}
		a.emit(ctx, ch, a.makeDoneEvent(totalInputTokens, totalOutputTokens, exitReason))
		return stepEnded
	}

	// Inject budget nudge if provided and not already emitted.
	if dec.NudgeMessage != "" && !ls.nudgeEmitted {
		a.conversation.AddUser(dec.NudgeMessage)
		ls.nudgeEmitted = true
	}

	signature := pendingToolSignature(pendingTools)
	if ls.recordToolSignature(signature, hasTextContent(blocks)) {
		a.emit(ctx, ch, TurnEvent{Type: "error", Error: fmt.Errorf("detected no progress after %d repeated tool-only rounds", ls.repeatedToolRounds)})
		a.emit(ctx, ch, a.makeDoneEvent(totalInputTokens, totalOutputTokens, agentsdk.ExitNoProgress))
		return stepEnded
	}

	// Check if the model signaled task completion via task_complete tool.
	// All sibling tools in the same batch are executed before exiting —
	// the model often pairs task_complete with a final write or commit.
	for _, tc := range pendingTools {
		if tc.Name == tools.TaskCompleteName {
			// If the batch is cancelled part-way, task_complete itself may
			// never run, so the turn did not complete the task — report the
			// cancellation the way the main path below does. The sweeper
			// inside the wrapper seals whatever tool_use blocks were left
			// unanswered, keeping the conversation protocol-valid for resume.
			//
			// This does not contradict the after-response hook, which was
			// handed task_complete by finalResponseReason before execution:
			// that says why this response is the turn's last, while the done
			// event says how the turn ended. Two questions, two values.
			if cancelled := a.executeToolsSealingCancellation(ctx, ch, pendingTools, streamedResults); cancelled {
				joinBackgroundTasks()
				a.emit(ctx, ch, TurnEvent{Type: "error", Error: ctx.Err()})
				a.emit(ctx, ch, a.makeDoneEvent(totalInputTokens, totalOutputTokens, agentsdk.ExitCancelled))
				return stepEnded
			}
			joinBackgroundTasks()
			a.emit(ctx, ch, a.makeDoneEvent(totalInputTokens, totalOutputTokens, agentsdk.ExitTaskComplete))
			return stepEnded
		}
	}

	// Execute tool calls — parallelize auto-approved tools when possible.
	if cancelled := a.executeToolsSealingCancellation(ctx, ch, pendingTools, streamedResults); cancelled {
		joinBackgroundTasks()
		a.emit(ctx, ch, TurnEvent{Type: "error", Error: ctx.Err()})
		a.emit(ctx, ch, a.makeDoneEvent(totalInputTokens, totalOutputTokens, agentsdk.ExitCancelled))
		return stepEnded
	}

	// Drain any pending wake events from background subagents.
	a.drainWakeEvents(ctx, ch)

	// Session memory extraction rides the background-task joins run by
	// the caller after stepProceed (and by the terminal paths above), so
	// terminal tool turns count toward extraction too — no inline
	// dispatch here.

	// Re-measure after tool execution so the context window status reflects
	// the current conversation state (tool results may have grown messages).
	a.context.MeasureUsage(a.conversation, systemPrompt, skillPromptText, activeTools)
	status := a.windowManager.Status()
	if status.WarningLevel != WarningNone && !ls.nudgeEmitted {
		a.conversation.AddUser(status.Advice)
		ls.nudgeEmitted = true
	}

	return stepProceed
}

// executeToolsSealingCancellation runs a pending tool batch and, if it was
// cancelled part-way, seals the tool_use blocks that never got a result and
// persists the repaired conversation. The wire protocol requires every
// tool_use to be answered, so an unsealed batch makes the next provider call
// fail a protocol check. It reports whether the batch was cancelled.
//
// All three of runToolPhase's execution sites — budget stop, task_complete,
// and the main path — need the same sealing, so they share this wrapper rather
// than each remembering to check executeTools' return.
//
// The snapshot save matters because executeTools returns early on
// cancellation, skipping the one it normally performs after a full batch. Left
// alone, the newest snapshot would still be the pre-provider one Turn wrote —
// and loadSessionHistory prefers a snapshot over the message log whenever one
// exists, so resuming would restore the session from before the assistant turn
// and silently drop the tool results that did complete.
func (a *Agent) executeToolsSealingCancellation(ctx context.Context, ch chan<- TurnEvent, pendingTools []provider.ToolUseBlock, streamedResults map[string]toolExecResult) bool {
	cancelled := a.executeTools(ctx, ch, pendingTools, streamedResults)
	if cancelled {
		synthesizeMissingToolResults(a.conversation, orphanReasonToolCancel)
		a.saveSnapshotIfNeeded()
	}
	return cancelled
}
