package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoopState_CanRecoverMaxTokens(t *testing.T) {
	ls := newLoopState(10, 0)
	assert.True(t, ls.canRecoverMaxTokens())
	ls.maxTokensRecoveryAttempts = 3
	assert.False(t, ls.canRecoverMaxTokens())
}

func TestLoopState_IncrementRecovery(t *testing.T) {
	ls := newLoopState(10, 0)
	ls.incrementMaxTokensRecovery()
	assert.Equal(t, 1, ls.maxTokensRecoveryAttempts)
	ls.incrementMaxTokensRecovery()
	assert.Equal(t, 2, ls.maxTokensRecoveryAttempts)
}

func TestLoopState_HasMoreTurns(t *testing.T) {
	ls := newLoopState(2, 0)
	assert.True(t, ls.hasMoreTurns())
	ls.turnCount = 2
	assert.False(t, ls.hasMoreTurns())
}

func TestLoopState_ResetPerTurn(t *testing.T) {
	ls := newLoopState(10, 0)
	ls.streamErr = true
	ls.resetPerTurn()
	assert.False(t, ls.streamErr)
}
