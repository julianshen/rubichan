package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewLoopState(t *testing.T) {
	ls := newLoopState(10, 3)
	assert.Equal(t, 10, ls.maxTurns)
	assert.Equal(t, 3, ls.turnCount)
	assert.Equal(t, 0, ls.repeatedToolRounds)
	assert.Equal(t, "", ls.lastToolSignature)
	assert.False(t, ls.streamErr)
}

func TestLoopState_HasMoreTurns(t *testing.T) {
	ls := newLoopState(5, 3)
	assert.True(t, ls.hasMoreTurns(), "turnCount=3, maxTurns=5: has more")

	ls.turnCount = 4
	assert.True(t, ls.hasMoreTurns(), "turnCount=4, maxTurns=5: one turn remains")

	ls.turnCount = 5
	assert.False(t, ls.hasMoreTurns(), "turnCount=5, maxTurns=5: no more turns")
}

func TestLoopState_ResetPerTurn(t *testing.T) {
	ls := newLoopState(10, 0)
	ls.streamErr = true
	ls.repeatedToolRounds = 2
	ls.lastToolSignature = "read_file"
	ls.resetPerTurn()
	assert.False(t, ls.streamErr, "streamErr should be cleared")
	assert.Equal(t, 2, ls.repeatedToolRounds, "cross-turn fields preserved")
	assert.Equal(t, "read_file", ls.lastToolSignature, "cross-turn fields preserved")
}

func TestLoopState_RecordToolSignature_NoProgress(t *testing.T) {
	ls := newLoopState(10, 0)

	assert.False(t, ls.recordToolSignature("read_file", false), "first occurrence")
	assert.False(t, ls.recordToolSignature("read_file", false), "second occurrence")
	assert.True(t, ls.recordToolSignature("read_file", false), "third occurrence triggers no-progress")
}

func TestLoopState_RecordToolSignature_ResetsOnNewSignature(t *testing.T) {
	ls := newLoopState(10, 0)

	ls.recordToolSignature("read_file", false)
	ls.recordToolSignature("read_file", false)
	assert.False(t, ls.recordToolSignature("write_file", false), "new signature resets counter")
}

func TestLoopState_RecordToolSignature_ResetsOnTextContent(t *testing.T) {
	ls := newLoopState(10, 0)

	ls.recordToolSignature("read_file", false)
	ls.recordToolSignature("read_file", false)
	assert.False(t, ls.recordToolSignature("read_file", true), "text content resets counter")
}

func TestLoopState_CheckDiminishingReturns_BelowThreshold(t *testing.T) {
	ls := newLoopState(50, 0)

	assert.False(t, ls.checkDiminishingReturns(100), "turn 1: not enough continuations")
	assert.False(t, ls.checkDiminishingReturns(200), "turn 2: not enough continuations")
	assert.False(t, ls.checkDiminishingReturns(250), "turn 3: delta=50 < threshold but need one more check")
	assert.True(t, ls.checkDiminishingReturns(300), "turn 4: delta=50 AND lastDelta=50, both < threshold")
}

func TestLoopState_CheckDiminishingReturns_AboveThreshold(t *testing.T) {
	ls := newLoopState(50, 0)

	assert.False(t, ls.checkDiminishingReturns(100))
	assert.False(t, ls.checkDiminishingReturns(800))
	assert.False(t, ls.checkDiminishingReturns(1500))
	assert.False(t, ls.checkDiminishingReturns(2500))
	assert.False(t, ls.checkDiminishingReturns(3500))
}

func TestLoopState_CheckDiminishingReturns_ResetsOnSpike(t *testing.T) {
	ls := newLoopState(50, 0)

	assert.False(t, ls.checkDiminishingReturns(100))
	assert.False(t, ls.checkDiminishingReturns(200))
	assert.False(t, ls.checkDiminishingReturns(250))

	assert.False(t, ls.checkDiminishingReturns(2000), "big spike resets the pattern")
	assert.False(t, ls.checkDiminishingReturns(2050))
	assert.False(t, ls.checkDiminishingReturns(2100))
	assert.False(t, ls.checkDiminishingReturns(2150))
}

func TestContinueReason_String(t *testing.T) {
	tests := []struct {
		reason ContinueReason
		want   string
	}{
		{ContinueNextTurn, "next_turn"},
		{ContinuePromptTooLongRetry, "prompt_too_long_retry"},
		{ContinueMaxTokensRecovery, "max_tokens_recovery"},
		{ContinueModelFallback, "model_fallback"},
		{ContinueUnknown, "unknown"},
		{ContinueReason(99), "unknown"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.reason.String())
	}
}
