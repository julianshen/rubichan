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

func newLoopState(maxTurns int) *loopState {
	return &loopState{maxTurns: maxTurns}
}

func (s *loopState) canRecoverMaxTokens() bool {
	return s.maxTokensRecoveryAttempts < maxOutputTokensRecoveryLimit
}

func (s *loopState) incrementMaxTokensRecovery() {
	s.maxTokensRecoveryAttempts++
}

func (s *loopState) shouldExit() bool {
	return s.turnCount >= s.maxTurns
}
