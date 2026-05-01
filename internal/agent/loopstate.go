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
	ContinueUnknown ContinueReason = iota
	// ContinueNextTurn means the model requested tool calls; the loop
	// will execute them and start another turn with the results.
	ContinueNextTurn
	// ContinuePromptTooLongRetry means the context was compacted after
	// a prompt-too-long error; the loop retries the same request.
	ContinuePromptTooLongRetry
	// ContinueMaxTokensRecovery means the model hit its output limit;
	// the loop sends a continuation prompt to resume generation.
	ContinueMaxTokensRecovery
	// ContinueModelFallback means the primary model failed (overloaded
	// or unavailable) and the loop retried with a fallback model.
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
