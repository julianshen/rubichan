package agentsdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContextBudgetEffectiveWindow(t *testing.T) {
	b := ContextBudget{Total: 100000, MaxOutputTokens: 4096}
	assert.Equal(t, 95904, b.EffectiveWindow())
}

func TestContextBudgetEffectiveWindowNegative(t *testing.T) {
	b := ContextBudget{Total: 1000, MaxOutputTokens: 5000}
	assert.Equal(t, 0, b.EffectiveWindow())
}

func TestContextBudgetUsedTokens(t *testing.T) {
	b := ContextBudget{
		SystemPrompt:     1000,
		SkillPrompts:     500,
		ToolDescriptions: 200,
		Conversation:     3000,
	}
	assert.Equal(t, 4700, b.UsedTokens())
}

func TestContextBudgetRemainingTokens(t *testing.T) {
	b := ContextBudget{
		Total:           100000,
		MaxOutputTokens: 4096,
		Conversation:    50000,
	}
	assert.Equal(t, 95904-50000, b.RemainingTokens())
}

func TestContextBudgetUsedPercentage(t *testing.T) {
	b := ContextBudget{
		Total:           100000,
		MaxOutputTokens: 0,
		Conversation:    50000,
	}
	assert.InDelta(t, 0.5, b.UsedPercentage(), 0.001)
}

func TestContextBudgetUsedPercentageZeroWindow(t *testing.T) {
	b := ContextBudget{Total: 0, MaxOutputTokens: 100}
	assert.Equal(t, 1.0, b.UsedPercentage())
}

func TestCompactResultFields(t *testing.T) {
	r := CompactResult{
		BeforeTokens:   10000,
		AfterTokens:    5000,
		BeforeMsgCount: 20,
		AfterMsgCount:  10,
		StrategiesRun:  []string{"truncate"},
	}
	assert.Equal(t, 10000, r.BeforeTokens)
	assert.Equal(t, 5000, r.AfterTokens)
	assert.Equal(t, []string{"truncate"}, r.StrategiesRun)
}
