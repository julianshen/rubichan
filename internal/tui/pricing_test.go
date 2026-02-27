package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEstimateCost(t *testing.T) {
	cost := EstimateCost("claude-sonnet-4-5", 1000, 500)
	assert.Greater(t, cost, 0.0)

	cost = EstimateCost("unknown-model", 1000, 500)
	assert.Equal(t, 0.0, cost)
}

func TestEstimateCostKnownModels(t *testing.T) {
	// claude-sonnet-4-5: $3/M input, $15/M output
	// 1M input + 1M output = $3 + $15 = $18
	cost := EstimateCost("claude-sonnet-4-5", 1_000_000, 1_000_000)
	assert.InDelta(t, 18.0, cost, 0.001)

	// claude-opus-4-5: $15/M input, $75/M output
	cost = EstimateCost("claude-opus-4-5", 1_000_000, 1_000_000)
	assert.InDelta(t, 90.0, cost, 0.001)

	// gpt-4o-mini: $0.15/M input, $0.60/M output
	cost = EstimateCost("gpt-4o-mini", 1_000_000, 1_000_000)
	assert.InDelta(t, 0.75, cost, 0.001)
}

func TestEstimateCostZeroTokens(t *testing.T) {
	cost := EstimateCost("claude-sonnet-4-5", 0, 0)
	assert.Equal(t, 0.0, cost)
}
