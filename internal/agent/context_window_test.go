package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContextWindowManager_Status(t *testing.T) {
	cm := NewContextManager(1000, 100)
	cwm := NewContextWindowManager(cm)

	cm.MeasureUsage(NewConversation(""), "test", "", nil)
	cm.SetBudget(1000)

	status := cwm.Status()
	assert.Equal(t, 1000, status.Total)
	assert.Equal(t, 900, status.EffectiveWindow())
	assert.Equal(t, WarningNone, status.WarningLevel)
	assert.Empty(t, status.Advice)
}

func TestContextWindowManager_Status_WarningLevels(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*ContextManager)
		wantLevel  WarningLevel
		wantAdvice bool
	}{
		{
			name: "none",
			setup: func(cm *ContextManager) {
				cm.MeasureUsage(NewConversation(""), "test", "", nil)
			},
			wantLevel:  WarningNone,
			wantAdvice: false,
		},
		{
			name: "low",
			setup: func(cm *ContextManager) {
				cm.SetBudget(1000)
				cm.budget.SystemPrompt = 630
				cm.budget.SkillPrompts = 0
				cm.budget.ToolDescriptions = 0
				cm.budget.Conversation = 70
			},
			wantLevel:  WarningLow,
			wantAdvice: true,
		},
		{
			name: "medium",
			setup: func(cm *ContextManager) {
				cm.SetBudget(1000)
				cm.budget.SystemPrompt = 720
				cm.budget.SkillPrompts = 0
				cm.budget.ToolDescriptions = 0
				cm.budget.Conversation = 80
			},
			wantLevel:  WarningMedium,
			wantAdvice: true,
		},
		{
			name: "high",
			setup: func(cm *ContextManager) {
				cm.SetBudget(1000)
				cm.budget.SystemPrompt = 750
				cm.budget.SkillPrompts = 0
				cm.budget.ToolDescriptions = 0
				cm.budget.Conversation = 105
			},
			wantLevel:  WarningHigh,
			wantAdvice: true,
		},
		{
			name: "critical",
			setup: func(cm *ContextManager) {
				cm.SetBudget(1000)
				cm.budget.SystemPrompt = 882
				cm.budget.SkillPrompts = 0
				cm.budget.ToolDescriptions = 0
				cm.budget.Conversation = 100
			},
			wantLevel:  WarningCritical,
			wantAdvice: true,
		},
		{
			name: "zero window with usage",
			setup: func(cm *ContextManager) {
				cm.SetBudget(50)
				cm.budget.MaxOutputTokens = 50
				cm.budget.SystemPrompt = 100
			},
			wantLevel:  WarningCritical,
			wantAdvice: true,
		},
		{
			name: "zero window zero usage",
			setup: func(cm *ContextManager) {
				cm.SetBudget(50)
				cm.budget.MaxOutputTokens = 50
			},
			wantLevel:  WarningCritical,
			wantAdvice: true,
		},
		{
			name: "over budget",
			setup: func(cm *ContextManager) {
				cm.SetBudget(1000)
				cm.budget.SystemPrompt = 1200
			},
			wantLevel:  WarningCritical,
			wantAdvice: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := NewContextManager(1000, 100)
			cwm := NewContextWindowManager(cm)
			tt.setup(cm)

			status := cwm.Status()
			assert.Equal(t, tt.wantLevel, status.WarningLevel, "level mismatch")
			if tt.wantAdvice {
				assert.NotEmpty(t, status.Advice, "expected advice")
				assert.Contains(t, status.Advice, "Context at")
			} else {
				assert.Empty(t, status.Advice, "expected no advice")
			}
		})
	}
}

func TestContextWindowManager_Status_CustomThresholds(t *testing.T) {
	cm := NewContextManager(1000, 100)
	cwm := NewContextWindowManager(cm)
	cm.SetThresholds(0.50, 0.60, 0.90, 0.95)
	cm.SetBudget(1000)

	// At custom warn threshold (0.50 = 450/900) -> Low
	cm.budget.SystemPrompt = 450
	cm.budget.Conversation = 0
	assert.Equal(t, WarningLow, cwm.Status().WarningLevel)

	// Just below custom caution threshold -> Low
	cm.budget.SystemPrompt = 539
	assert.Equal(t, WarningLow, cwm.Status().WarningLevel)

	// At custom caution threshold (0.60 = 540/900) -> Medium
	cm.budget.SystemPrompt = 540
	assert.Equal(t, WarningMedium, cwm.Status().WarningLevel)

	// Just below custom trigger -> Medium
	cm.budget.SystemPrompt = 809
	assert.Equal(t, WarningMedium, cwm.Status().WarningLevel)

	// At custom trigger (0.90 = 810/900) -> High
	cm.budget.SystemPrompt = 810
	assert.Equal(t, WarningHigh, cwm.Status().WarningLevel)

	// Just below custom hardBlock -> High
	cm.budget.SystemPrompt = 854
	assert.Equal(t, WarningHigh, cwm.Status().WarningLevel)

	// At custom hardBlock (0.95 = 855/900) -> Critical
	cm.budget.SystemPrompt = 855
	assert.Equal(t, WarningCritical, cwm.Status().WarningLevel)
}

func TestWarningLevel_String(t *testing.T) {
	assert.Equal(t, "none", WarningNone.String())
	assert.Equal(t, "low", WarningLow.String())
	assert.Equal(t, "medium", WarningMedium.String())
	assert.Equal(t, "high", WarningHigh.String())
	assert.Equal(t, "critical", WarningCritical.String())
	assert.Equal(t, "unknown", WarningLevel(99).String())
	assert.Equal(t, "unknown", WarningLevel(-1).String())
}

func TestAdviceForLevel(t *testing.T) {
	assert.Contains(t, adviceForLevel(WarningCritical, 0.99), "99%")
	assert.Contains(t, adviceForLevel(WarningCritical, 0.99), "compacted aggressively")
	assert.Contains(t, adviceForLevel(WarningHigh, 0.96), "96%")
	assert.Contains(t, adviceForLevel(WarningHigh, 0.96), "auto-compaction")
	assert.Contains(t, adviceForLevel(WarningMedium, 0.85), "85%")
	assert.Contains(t, adviceForLevel(WarningMedium, 0.85), "approaching limit")
	assert.Contains(t, adviceForLevel(WarningLow, 0.75), "75%")
	assert.Contains(t, adviceForLevel(WarningLow, 0.75), "healthy but growing")
	assert.Empty(t, adviceForLevel(WarningNone, 0.5))
}

func TestAdviceForLevel_Rounding(t *testing.T) {
	assert.Contains(t, adviceForLevel(WarningHigh, 0.994), "99%")
	assert.Contains(t, adviceForLevel(WarningCritical, 0.995), "100%")
	assert.Contains(t, adviceForLevel(WarningCritical, 1.0), "100%")
}

func TestNewContextWindowManager_NilPanics(t *testing.T) {
	assert.Panics(t, func() {
		NewContextWindowManager(nil)
	})
}

func TestContextWindowManager_Status_BoundaryPercentages(t *testing.T) {
	tests := []struct {
		name      string
		pct       float64
		wantLevel WarningLevel
	}{
		{"exactly 70%", 0.70, WarningLow},
		{"exactly 80%", 0.80, WarningMedium},
		{"exactly 95%", 0.95, WarningHigh},
		{"exactly 98%", 0.98, WarningCritical},
		{"just below 70%", 0.69, WarningNone},
		{"just below 80%", 0.79, WarningLow},
		{"just below 95%", 0.94, WarningMedium},
		{"just below 98%", 0.97, WarningHigh},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := NewContextManager(1000, 100)
			cwm := NewContextWindowManager(cm)
			cm.SetBudget(1000)
			cm.budget.SystemPrompt = int(tt.pct * 900)
			cm.budget.Conversation = 0

			status := cwm.Status()
			assert.Equal(t, tt.wantLevel, status.WarningLevel)
		})
	}
}
