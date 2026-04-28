package agent

// maxPromptTooLongRetries caps total reactive-compaction rounds within a
// single Turn() call, including rounds where compaction succeeded but the
// provider still rejected the request.
const maxPromptTooLongRetries = 3

type loopState struct {
	maxTurns              int
	turnCount             int
	repeatedToolRounds    int
	lastToolSignature     string
	streamErr             bool
	promptTooLongAttempts int
}

func newLoopState(maxTurns, turnCount int) *loopState {
	return &loopState{maxTurns: maxTurns, turnCount: turnCount}
}

func (s *loopState) hasMoreTurns() bool {
	return s.turnCount < s.maxTurns
}

// resetPerTurn clears per-iteration state. Cross-turn fields
// (repeatedToolRounds, lastToolSignature) are intentionally preserved.
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
