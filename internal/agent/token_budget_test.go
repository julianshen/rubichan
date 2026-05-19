package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseTokenBudget_ShorthandStart(t *testing.T) {
	val, ok := ParseTokenBudget("+500k do this thing")
	assert.True(t, ok)
	assert.Equal(t, 500_000, val)
}

func TestParseTokenBudget_ShorthandStartDecimal(t *testing.T) {
	val, ok := ParseTokenBudget("+2.5m write a long analysis")
	assert.True(t, ok)
	assert.Equal(t, 2_500_000, val)
}

func TestParseTokenBudget_ShorthandEnd(t *testing.T) {
	val, ok := ParseTokenBudget("do this thing +500k")
	assert.True(t, ok)
	assert.Equal(t, 500_000, val)
}

func TestParseTokenBudget_ShorthandEndWithPunctuation(t *testing.T) {
	val, ok := ParseTokenBudget("do this thing +1m.")
	assert.True(t, ok)
	assert.Equal(t, 1_000_000, val)
}

func TestParseTokenBudget_Verbose(t *testing.T) {
	val, ok := ParseTokenBudget("please use 2M tokens for this")
	assert.True(t, ok)
	assert.Equal(t, 2_000_000, val)
}

func TestParseTokenBudget_VerboseSpend(t *testing.T) {
	val, ok := ParseTokenBudget("spend 500k tokens on the analysis")
	assert.True(t, ok)
	assert.Equal(t, 500_000, val)
}

func TestParseTokenBudget_VerboseCaseInsensitive(t *testing.T) {
	val, ok := ParseTokenBudget("USE 1B TOKENS")
	assert.True(t, ok)
	assert.Equal(t, 1_000_000_000, val)
}

func TestParseTokenBudget_NoDirective(t *testing.T) {
	val, ok := ParseTokenBudget("just a normal message")
	assert.False(t, ok)
	assert.Equal(t, 0, val)
}

func TestParseTokenBudget_NaturalLanguageFalsePositive(t *testing.T) {
	// "+500k" in the middle should not match.
	val, ok := ParseTokenBudget("the +500k threshold is high")
	assert.False(t, ok)
	assert.Equal(t, 0, val)
}

func TestParseTokenBudget_PriorityStartOverEnd(t *testing.T) {
	// Start shorthand takes priority.
	val, ok := ParseTokenBudget("+100k do this +200k")
	assert.True(t, ok)
	assert.Equal(t, 100_000, val)
}

func TestStripTokenBudget(t *testing.T) {
	assert.Equal(t, "do this thing", StripTokenBudget("+500k do this thing"))
	assert.Equal(t, "do this thing", StripTokenBudget("do this thing +500k"))
	assert.Equal(t, "please  for this", StripTokenBudget("please use 2M tokens for this"))
	assert.Equal(t, "just a normal message", StripTokenBudget("just a normal message"))
}
