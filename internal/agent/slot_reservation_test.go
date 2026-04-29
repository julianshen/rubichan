package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEffectiveMaxTokens_Default(t *testing.T) {
	a := &Agent{configuredMaxTokens: 0}
	ls := newLoopState(50, 0)
	assert.Equal(t, defaultMaxOutputTokens, a.effectiveMaxTokens(ls))
}

func TestEffectiveMaxTokens_Configured(t *testing.T) {
	a := &Agent{configuredMaxTokens: 16384}
	ls := newLoopState(50, 0)
	assert.Equal(t, 16384, a.effectiveMaxTokens(ls))
}

func TestEffectiveMaxTokens_Escalated(t *testing.T) {
	a := &Agent{configuredMaxTokens: 0}
	ls := newLoopState(50, 0)
	ls.tokensEscalated = true
	assert.Equal(t, escalatedMaxOutputTokens, a.effectiveMaxTokens(ls))
}

func TestEffectiveMaxTokens_EscalatedOverridesConfig(t *testing.T) {
	a := &Agent{configuredMaxTokens: 4096}
	ls := newLoopState(50, 0)
	ls.tokensEscalated = true
	assert.Equal(t, escalatedMaxOutputTokens, a.effectiveMaxTokens(ls))
}
