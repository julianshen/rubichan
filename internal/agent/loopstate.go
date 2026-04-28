package agent

// maxPromptTooLongRetries caps total reactive-compaction rounds within a
// single Turn() call, including rounds where compaction succeeded but the
// provider still rejected the request.
const maxPromptTooLongRetries = 3

// maxOutputTokensRecoveryLimit caps continuation retries when the model
// hits the output token limit on every attempt.
const maxOutputTokensRecoveryLimit = 3

// diminishingThreshold is the output-token delta below which a turn is
// considered to have made negligible progress. When 3+ consecutive turns
// stay below this threshold the loop exits with ExitDiminishingReturns.
const diminishingThreshold = 500

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
}

func newLoopState(maxTurns, turnCount int) *loopState {
	return &loopState{maxTurns: maxTurns, turnCount: turnCount}
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
