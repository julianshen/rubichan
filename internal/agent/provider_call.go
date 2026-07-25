package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/julianshen/rubichan/internal/agent/errorclass"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/provider/normalize"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// providerCallOutcome tells runLoop how to proceed after a provider call.
type providerCallOutcome int

const (
	// providerCallProceed: the stream is ready; process it.
	providerCallProceed providerCallOutcome = iota
	// providerCallRetryTurn: a recovery action (compaction, token bump)
	// mutated loop state; re-enter the loop without consuming a turn. The
	// callee has already decremented ls.turnCount to cancel the loop's
	// post-statement increment and set ls.lastContinueReason.
	providerCallRetryTurn
	// providerCallEnded: a terminal failure was emitted (error followed by
	// done); runLoop must return.
	providerCallEnded
)

// streamWithRecovery performs the foreground model call for one loop
// iteration: rate limiting, streaming with retry (and a non-streaming
// fallback for providers that support it), error classification, in-loop
// recovery for prompt-too-long / max-output-tokens, and the overloaded-model
// fallback. This is the recovery state machine the design doc contrasts with
// the SDK loop's simple overflow check — extracted from runLoop so the loop
// reads as orchestration and this machine can be reasoned about (and one day
// seamed) in isolation.
//
// The returned stream is non-nil only for providerCallProceed. Terminal
// paths emit their own error and done events before returning; totals are
// passed in solely so those done events carry accurate token usage.
func (a *Agent) streamWithRecovery(ctx context.Context, ch chan<- TurnEvent, ls *loopState, req provider.CompletionRequest, totalInputTokens, totalOutputTokens int) (<-chan provider.StreamEvent, providerCallOutcome) {
	if a.rateLimiter != nil {
		if !a.rateLimiter.AllowNow() {
			a.emit(ctx, ch, TurnEvent{Type: "rate_limited"})
		}
		if err := a.rateLimiter.Wait(ctx); err != nil {
			a.emit(ctx, ch, TurnEvent{Type: "error", Error: fmt.Errorf("rate limiter: %w", err)})
			a.emit(ctx, ch, a.makeDoneEvent(totalInputTokens, totalOutputTokens, agentsdk.ExitRateLimited))
			return nil, providerCallEnded
		}
	}

	retryCfg := TurnRetryConfig{
		Source: agentsdk.QuerySourceForeground, // user-facing query
	}
	// Providers that implement NonStreamer get a non-streaming
	// fallback wired in automatically. TurnRetry only invokes it
	// after all streaming attempts exhaust with retryable errors,
	// so the common path still hits the streaming endpoint first.
	if ns, ok := a.provider.(NonStreamer); ok {
		reqCopy := req
		retryCfg.NonStreamFallback = func(ctx context.Context) ([]provider.StreamEvent, error) {
			return ns.NonStream(ctx, reqCopy)
		}
	}
	onRetry := func(attempt int, delay time.Duration, cause error) {
		a.emit(ctx, ch, TurnEvent{
			Type:  "retrying",
			Error: fmt.Errorf("attempt %d after %s: %w", attempt, delay, cause),
		})
	}
	stream, err := TurnRetry(ctx, retryCfg, func(ctx context.Context) (<-chan provider.StreamEvent, error) {
		return a.provider.Stream(ctx, req)
	}, onRetry)
	if err != nil {
		class := errorclass.Classify(err)
		a.logger.Warn("provider error classified as %s: %v", class, err)

		if class == errorclass.ClassPromptTooLong || class == errorclass.ClassMaxOutputTokens {
			ls.withheldErrors.Add(class, err)
			if a.attemptRecovery(ctx, ch, ls, class, nil) {
				ls.withheldErrors.MarkRecovered(class)
				ls.turnCount--
				if class == errorclass.ClassPromptTooLong {
					ls.lastContinueReason = ContinuePromptTooLongRetry
				} else {
					ls.lastContinueReason = ContinueMaxTokensRecovery
				}
				return nil, providerCallRetryTurn
			}
			// Recovery exhausted — surface the withheld error with class context
			// so consumers can distinguish prompt-too-long from max_tokens without parsing strings.
			lastErr, ok := ls.withheldErrors.LastUnrecovered()
			if ok {
				a.emit(ctx, ch, TurnEvent{Type: "error", Error: fmt.Errorf("provider stream (%s): %w", lastErr.Class, lastErr.Err)})
			} else {
				// Buffer was cleared or marked recovered unexpectedly — still emit the original error
				// so the consumer always sees an error before done on failed recovery.
				a.emit(ctx, ch, TurnEvent{Type: "error", Error: fmt.Errorf("provider stream (%s): %w", class, err)})
			}
			ls.withheldErrors.Clear()
			exitReason := agentsdk.ExitContextOverflow
			if class == errorclass.ClassMaxOutputTokens {
				exitReason = agentsdk.ExitMaxOutputTokens
			}
			a.emit(ctx, ch, a.makeDoneEvent(totalInputTokens, totalOutputTokens, exitReason))
			return nil, providerCallEnded
		}

		if class == errorclass.ClassModelOverloaded && a.fallbackModel != "" {
			a.logger.Warn("primary model overloaded; retrying with fallback model %s", a.fallbackModel)
			a.emit(ctx, ch, TurnEvent{Type: "model_fallback", Model: a.fallbackModel})

			// Tombstone partial messages from the failed attempt so they
			// don't pollute the fallback model's context.
			tombstonedCount := a.conversation.TombstoneSinceLastAssistant(agentsdk.TombstoneReasonModelFallback)
			if tombstonedCount > 0 {
				a.logger.Warn("tombstoned %d partial messages before fallback", tombstonedCount)
			}

			fallbackReq := req
			fallbackReq.Model = a.fallbackModel
			fallbackReq.Messages = normalize.FilterTombstoned(stripThinkingBlocks(req.Messages))
			fallbackRetryCfg := TurnRetryConfig{}
			if ns, ok := a.provider.(NonStreamer); ok {
				fbReq := fallbackReq
				fallbackRetryCfg.NonStreamFallback = func(ctx context.Context) ([]provider.StreamEvent, error) {
					return ns.NonStream(ctx, fbReq)
				}
			}
			var fallbackErr error
			stream, fallbackErr = TurnRetry(ctx, fallbackRetryCfg, func(ctx context.Context) (<-chan provider.StreamEvent, error) {
				return a.provider.Stream(ctx, fallbackReq)
			}, onRetry)
			if fallbackErr == nil {
				ls.lastContinueReason = ContinueModelFallback
				return stream, providerCallProceed
			}
			a.logger.Warn("fallback model also failed: %v", fallbackErr)
			a.emit(ctx, ch, TurnEvent{Type: "error", Error: fmt.Errorf("provider stream (fallback): %w", fallbackErr)})
			a.emit(ctx, ch, a.makeDoneEvent(totalInputTokens, totalOutputTokens, agentsdk.ExitProviderError))
			return nil, providerCallEnded
		}

		a.emit(ctx, ch, TurnEvent{Type: "error", Error: fmt.Errorf("provider stream: %w", err)})
		a.emit(ctx, ch, a.makeDoneEvent(totalInputTokens, totalOutputTokens, agentsdk.ExitProviderError))
		return nil, providerCallEnded
	}

	return stream, providerCallProceed
}
