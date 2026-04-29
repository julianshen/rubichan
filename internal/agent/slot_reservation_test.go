package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEffectiveMaxTokens_FromConfig(t *testing.T) {
	ls := newLoopState(50, 0, 8192)
	assert.Equal(t, 8192, ls.maxOutputTokens)
}

func TestEffectiveMaxTokens_CustomValue(t *testing.T) {
	ls := newLoopState(50, 0, 16384)
	assert.Equal(t, 16384, ls.maxOutputTokens)
}

func TestEscalateMaxTokens(t *testing.T) {
	ls := newLoopState(50, 0, 8192)
	a := &Agent{}
	a.escalateMaxTokens(ls)
	assert.Equal(t, escalatedMaxOutputTokens, ls.maxOutputTokens)
}

func TestEscalateMaxTokens_OnlyOnce(t *testing.T) {
	ls := newLoopState(50, 0, 8192)
	a := &Agent{}
	a.escalateMaxTokens(ls)
	assert.Equal(t, escalatedMaxOutputTokens, ls.maxOutputTokens)
	a.escalateMaxTokens(ls)
	assert.Equal(t, escalatedMaxOutputTokens, ls.maxOutputTokens, "escalation should be idempotent")
}
