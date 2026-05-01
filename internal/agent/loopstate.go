package agent

// maxPromptTooLongRetries caps total reactive-compaction rounds within a
// single Turn() call, including rounds where compaction succeeded but the
// provider still rejected the request.
const maxPromptTooLongRetries = 3

// maxOutputTokensRecoveryLimit caps continuation retries when the model
// hits the output token limit on every attempt.
const maxOutputTokensRecoveryLimit = 3

// diminishingThreshold is the output-token delta below which a turn is
// considered to have made negligible progress. When 4 consecutive turns
// stay below this threshold the loop exits with ExitDiminishingReturns.
const diminishingThreshold = 500

const escalatedMaxOutputTokens = 65536

type ContinueReason int

const (
	// ContinueUnknown is the zero value before the loop determines why
	// the current turn ended. It should never appear in telemetry.
	ContinueUnknown ContinueReason = iota
	// ContinueNextTurn is the standard path: the model emitted tool calls
	// and the loop must execute them, append results, and start a new turn.
	// Distinguished from other reasons so telemetry can measure "normal"
	// turn frequency versus recovery-driven turns.
	ContinueNextTurn
	// ContinuePromptTooLongRetry triggers reactive compaction, which mutates
	// the conversation by replacing old messages with summaries. Tracked
	// separately because compaction changes context quality and should be
	// visible in metrics as a distinct cost center.
	ContinuePromptTooLongRetry
	// ContinueMaxTokensRecovery means the model stopped mid-generation due
	// to the output token limit. The loop sends a continuation prompt to
	// resume from where the model left off. Tracked separately because
	// it indicates the response was truncated and may need user review.
	ContinueMaxTokensRecovery
	// ContinueModelFallback means the primary provider failed with a
	// retryable error (overloaded, 529, etc.) and the loop fell back to
	// an alternate model. Tracked separately for provider reliability
	// metrics and to flag potential quality differences between models.
	ContinueModelFallback
)

func (r ContinueReason) String() string {
	switch r {
	case ContinueNextTurn:
		return "next_turn"
	case ContinuePromptTooLongRetry:
		return "prompt_too_long_retry"
	case ContinueMaxTokensRecovery:
		return "max_tokens_recovery"
	case ContinueModelFallback:
		return "model_fallback"
	default:
		return "unknown"
	}
}

type loopState struct {
	maxTurns                  int
	turnCount                 int
	repeatedToolRounds        int
	lastToolSignature         string
	streamErr                 bool
	promptTooLongAttempts     int
	maxTokensRecoveryAttempts int
	continuationCount         int
	lastDeltaTokens           int
	lastGlobalOutputTokens    int
	lastContinueReason        ContinueReason
	nudgeEmitted              bool
	maxOutputTokens           int
	withheldErrors            *withheldErrorBuffer
}

func newLoopState(maxTurns, turnCount, maxOutputTokens int) *loopState {
	return &loopState{
		maxTurns:        maxTurns,
		turnCount:       turnCount,
		maxOutputTokens: maxOutputTokens,
		withheldErrors:  &withheldErrorBuffer{},
	}
}

func (s *loopState) hasMoreTurns() bool {
	return s.turnCount < s.maxTurns
}

// resetPerTurn clears per-iteration state. Cross-turn fields
// (repeatedToolRounds, lastToolSignature, maxTokensRecoveryAttempts,
// continuationCount, lastDeltaTokens, lastGlobalOutputTokens)
// are intentionally preserved across sub-turns within a single Turn().
func (s *loopState) resetPerTurn() {
	s.streamErr = false
}

// recordToolSignature tracks consecutive identical tool-only rounds.
// Returns true when the no-progress threshold is exceeded.
func (s *loopState) recordToolSignature(sig string, hasText bool) bool {
	if !hasText && sig == s.lastToolSignature {
		s.repeatedToolRounds++
	} else {
		s.lastToolSignature = sig
		s.repeatedToolRounds = 1
	}
	return s.repeatedToolRounds >= maxRepeatedPendingToolRounds
}

func (s *loopState) checkDiminishingReturns(currentOutputTokens int) bool {
	delta := currentOutputTokens - s.lastGlobalOutputTokens
	if delta < 0 {
		delta = 0
	}
	isDiminishing := s.continuationCount >= 3 &&
		delta < diminishingThreshold &&
		s.lastDeltaTokens < diminishingThreshold
	s.lastDeltaTokens = delta
	s.lastGlobalOutputTokens = currentOutputTokens
	if delta >= diminishingThreshold {
		s.continuationCount = 0
	} else {
		s.continuationCount++
	}
	return isDiminishing
}
