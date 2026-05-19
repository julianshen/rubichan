package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseTokenBudget_ShorthandStart(t *testing.T) {
	val, stripped, ok := ParseTokenBudget("+500k do this thing")
	assert.True(t, ok)
	assert.Equal(t, 500_000, val)
	assert.Equal(t, "do this thing", stripped)
}

func TestParseTokenBudget_ShorthandStartDecimal(t *testing.T) {
	val, stripped, ok := ParseTokenBudget("+2.5m write a long analysis")
	assert.True(t, ok)
	assert.Equal(t, 2_500_000, val)
	assert.Equal(t, "write a long analysis", stripped)
}

func TestParseTokenBudget_ShorthandEnd(t *testing.T) {
	val, stripped, ok := ParseTokenBudget("do this thing +500k")
	assert.True(t, ok)
	assert.Equal(t, 500_000, val)
	assert.Equal(t, "do this thing", stripped)
}

func TestParseTokenBudget_ShorthandEndWithPunctuation(t *testing.T) {
	val, stripped, ok := ParseTokenBudget("do this thing +1m.")
	assert.True(t, ok)
	assert.Equal(t, 1_000_000, val)
	assert.Equal(t, "do this thing", stripped)
}

func TestParseTokenBudget_Verbose(t *testing.T) {
	val, stripped, ok := ParseTokenBudget("please use 2M tokens for this")
	assert.True(t, ok)
	assert.Equal(t, 2_000_000, val)
	assert.Equal(t, "please  for this", stripped)
}

func TestParseTokenBudget_VerboseSpend(t *testing.T) {
	val, stripped, ok := ParseTokenBudget("spend 500k tokens on the analysis")
	assert.True(t, ok)
	assert.Equal(t, 500_000, val)
	assert.Equal(t, "on the analysis", stripped)
}

func TestParseTokenBudget_VerboseCaseInsensitive(t *testing.T) {
	val, _, ok := ParseTokenBudget("USE 1B TOKENS")
	assert.True(t, ok)
	assert.Equal(t, 1_000_000_000, val)
}

func TestParseTokenBudget_NoDirective(t *testing.T) {
	val, stripped, ok := ParseTokenBudget("just a normal message")
	assert.False(t, ok)
	assert.Equal(t, 0, val)
	assert.Equal(t, "just a normal message", stripped)
}

func TestParseTokenBudget_NaturalLanguageFalsePositive(t *testing.T) {
	// "+500k" in the middle should not match.
	val, _, ok := ParseTokenBudget("the +500k threshold is high")
	assert.False(t, ok)
	assert.Equal(t, 0, val)
}

func TestParseTokenBudget_PriorityStartOverEnd(t *testing.T) {
	// Start shorthand takes priority.
	val, stripped, ok := ParseTokenBudget("+100k do this +200k")
	assert.True(t, ok)
	assert.Equal(t, 100_000, val)
	assert.Equal(t, "do this +200k", stripped)
}
