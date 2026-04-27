package agent

const maxOutputTokensRecoveryLimit = 3

type loopState struct {
	maxTurns                  int
	turnCount                 int
	maxTokensRecoveryAttempts int
	repeatedToolRounds        int
	lastToolSignature         string
	streamErr                 bool
}

func newLoopState(maxTurns, turnCount int) *loopState {
	return &loopState{maxTurns: maxTurns, turnCount: turnCount}
}

func (s *loopState) canRecoverMaxTokens() bool {
	return s.maxTokensRecoveryAttempts < maxOutputTokensRecoveryLimit
}

func (s *loopState) incrementMaxTokensRecovery() {
	s.maxTokensRecoveryAttempts++
}

func (s *loopState) hasMoreTurns() bool {
	return s.turnCount < s.maxTurns
}

func (s *loopState) resetPerTurn() {
	s.streamErr = false
}
