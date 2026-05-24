package agent

import (
	"fmt"
	"math"
)

type ContextWindowStatus struct {
	ContextBudget
	WarningLevel WarningLevel
	Advice       string
}

type WarningLevel int

const (
	WarningNone WarningLevel = iota
	WarningLow
	WarningMedium
	WarningHigh
	WarningCritical
)

var warningLevelNames = [...]string{"none", "low", "medium", "high", "critical"}

func (w WarningLevel) String() string {
	if w >= 0 && int(w) < len(warningLevelNames) {
		return warningLevelNames[w]
	}
	return "unknown"
}

type ContextWindowManager struct {
	cm *ContextManager
}

func NewContextWindowManager(cm *ContextManager) *ContextWindowManager {
	if cm == nil {
		panic("NewContextWindowManager: cm is nil")
	}
	return &ContextWindowManager{cm: cm}
}

func (cwm *ContextWindowManager) RecordUsage(usedTokens int) {
	// No-op: history tracking removed as YAGNI. Re-add when a consumer exists.
	_ = usedTokens
}

func (cwm *ContextWindowManager) Status() ContextWindowStatus {
	budget, warn, caution, trigger, hardBlock := cwm.cm.BudgetWithThresholds()
	pct := budget.UsedPercentage()
	level := warningLevelForPercentage(pct, warn, caution, trigger, hardBlock)

	return ContextWindowStatus{
		ContextBudget: budget,
		WarningLevel:  level,
		Advice:        adviceForLevel(level, pct),
	}
}

func warningLevelForPercentage(pct, warn, caution, trigger, hardBlock float64) WarningLevel {
	switch {
	case pct >= hardBlock:
		return WarningCritical
	case pct >= trigger:
		return WarningHigh
	case pct >= caution:
		return WarningMedium
	case pct >= warn:
		return WarningLow
	default:
		return WarningNone
	}
}

func adviceForLevel(level WarningLevel, pct float64) string {
	p := int(math.Round(pct * 100))
	prefix := fmt.Sprintf("Context at %d%% — ", p)

	switch level {
	case WarningCritical:
		return prefix + "conversation will be compacted aggressively. Important older messages may be lost. Consider starting a new session or compacting manually."
	case WarningHigh:
		return prefix + "auto-compaction is active. Older tool results and messages are being summarized to make room."
	case WarningMedium:
		return prefix + "approaching limit. Consider compacting to control what gets preserved, or start a new session."
	case WarningLow:
		return prefix + "healthy but growing. Long conversations will eventually need compaction."
	default:
		return ""
	}
}
