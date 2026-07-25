package agent

import (
	"context"
	"fmt"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/text"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// consumedStream bundles what one provider stream produced: the accumulator
// holding content blocks and pending tool calls, the streaming-dispatch
// executor (which may already be running concurrency-safe tools), any
// extended-thinking text, and the provider's stop reason. Stream errors are
// recorded on the loop state (ls.streamErr), not here.
type consumedStream struct {
	acc         *agentsdk.StreamAccumulator
	execStream  *streamingToolExecutor
	thinkingBuf string
	stopReason  string
}

// consumeProviderStream drains one provider stream into an accumulator:
// per-turn state reset, mid-stream dispatch of concurrency-safe
// auto-approved tools (with write/unknown/unsafe tools acting as ordering
// barriers), token-usage accounting, cache-break detection, and stop-reason
// capture. Extracted from runLoop so the loop reads as orchestration;
// behavior is unchanged.
//
// Token totals are accumulated through pointers because every stream event
// contributes usage and the caller's terminal paths must report totals the
// callee has already counted.
func (a *Agent) consumeProviderStream(ctx context.Context, ch chan<- TurnEvent, ls *loopState, stream <-chan provider.StreamEvent, totalInputTokens, totalOutputTokens *int) consumedStream {
	// Accumulate assistant content blocks and track tool calls via the
	// shared StreamAccumulator (pkg/agentsdk).
	ls.resetPerTurn()
	var thinkingBuf string
	var stopReason string

	// Streaming dispatch: concurrency-safe tools run in the
	// background as their tool_use blocks finalize during the stream,
	// overlapping with any remaining model text.
	execStream := newStreamingToolExecutor(maxParallelTools, func(sctx context.Context, tc provider.ToolUseBlock) toolExecResult {
		return a.executeSingleTool(sctx, ch, tc)
	})

	acc := agentsdk.NewStreamAccumulator()
	// Commit text only if it is not purely whitespace. This prevents
	// whitespace-only responses from polluting the conversation. Note:
	// LLMCompleter.Complete() also validates empty responses at its layer,
	// failing fast with an error. The agent handles empty responses
	// gracefully (adding a placeholder message below), allowing the turn
	// to continue. This design enables callers of Complete() to fail fast
	// (e.g., wiki diagram generation), while agent turns can recover with
	// a placeholder message to keep conversation valid.
	acc.KeepText = func(s string) bool { return !text.IsEmptyResponse(s) }
	// Streaming dispatch: if the finalized tool is concurrency-safe
	// and auto-approved, start executing it now so it runs in parallel
	// with the remaining model stream. The marker check comes first so
	// non-eligible tools skip the approval scan entirely — approval can
	// be expensive when a security scanner is wired in.
	acc.OnToolFinalized = func(tc provider.ToolUseBlock) {
		tool, ok := a.tools.Get(tc.Name)
		if !ok {
			// Unknown tool — conservative ordering barrier.
			execStream.SetBarrier()
			return
		}
		isUnsafe := true
		isWrite := false
		dispatched := false
		if ic, icok := tool.(agentsdk.InputConcurrencySafeTool); icok {
			isWrite = isWriteOperationForInput(ic, tc.Input)
			if ic.IsConcurrencySafeForInput(tc.Input) {
				approval := a.approvalResultForTool(tc)
				if approval == AutoApproved || approval == TrustRuleApproved {
					dispatched = execStream.Dispatch(ctx, tc)
				}
				isUnsafe = false
			}
		} else if cs, csok := tool.(agentsdk.ConcurrencySafeTool); csok && cs.IsConcurrencySafe() {
			isWrite = isWriteOperation(tool, tc.Input)
			approval := a.approvalResultForTool(tc)
			if approval == AutoApproved || approval == TrustRuleApproved {
				dispatched = execStream.Dispatch(ctx, tc)
			}
			isUnsafe = false
		} else {
			// Not concurrency-safe — still check write status for barrier.
			isWrite = isWriteOperation(tool, tc.Input)
		}
		// A write tool acts as an ordering barrier even when dispatched:
		// subsequent safe tools must wait for it to complete.
		if isWrite {
			execStream.SetBarrier()
		}
		if !dispatched && isUnsafe {
			execStream.SetBarrier()
		}
	}

	for event := range stream {
		// Accumulate token usage from every stream event.
		*totalInputTokens += event.InputTokens
		*totalOutputTokens += event.OutputTokens

		// Detect prompt cache breaks on message_start.
		if event.Type == agentsdk.EventMessageStart && a.cacheBreakDetector != nil {
			if report := a.cacheBreakDetector.RecordUsage(ls.turnCount, event.CacheReadTokens); report != nil {
				a.logger.Warn("cache break detected: %s", report.Diagnosis)
			}
		}

		switch event.Type {
		case "thinking_delta":
			thinkingBuf += event.Text
			a.emit(ctx, ch, TurnEvent{Type: "thinking_delta", Text: event.Text})

		case "text_delta":
			// During tool accumulation, text deltas carry input JSON
			// fragments; only regular text is emitted to the consumer.
			if !acc.AddText(event.Text) {
				a.emit(ctx, ch, TurnEvent{Type: "text_delta", Text: event.Text})
			}

		case "tool_use":
			if event.ToolUse == nil {
				// Finalize any in-progress text/tool to prevent input corruption.
				acc.Finish()
				a.emit(ctx, ch, TurnEvent{Type: "error", Error: fmt.Errorf("provider sent tool_use event with nil ToolUse")})
				continue
			}
			// Finalizes any pending text and previous tool, then starts
			// new tool accumulation. Input may be empty here if the
			// provider will deliver it as subsequent text_delta events
			// (legacy path) — otherwise the next content_block_stop or
			// tool_use event triggers finalize.
			acc.StartTool(*event.ToolUse)

		case agentsdk.EventContentBlockStop:
			// Finalize on block-end so single-tool responses
			// dispatch mid-stream. Providers that don't emit this
			// fall back to the "finalize on next tool_use or stream
			// end" timing, which is the legacy multi-tool path.
			acc.FinalizeTool()

		case "error":
			ls.streamErr = true
			a.emit(ctx, ch, TurnEvent{Type: "error", Error: event.Error})

		case "stop":
			// Capture stop reason for post-loop detection (e.g. max_tokens truncation).
			if event.StopReason != "" {
				stopReason = event.StopReason
			}
		}
	}
	return consumedStream{acc: acc, execStream: execStream, thinkingBuf: thinkingBuf, stopReason: stopReason}
}
