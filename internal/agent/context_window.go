package agent

import (
	"fmt"
	"math"
	"sync"
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
	cm      *ContextManager
	mu      sync.RWMutex
	history []usageSample
}

type usageSample struct {
	usedTokens int
}

func NewContextWindowManager(cm *ContextManager) *ContextWindowManager {
	if cm == nil {
		panic("NewContextWindowManager: cm is nil")
	}
	return &ContextWindowManager{
		cm:      cm,
		history: make([]usageSample, 0, 100),
	}
}

func (cwm *ContextWindowManager) RecordUsage(usedTokens int) {
	cwm.mu.Lock()
	cwm.history = append(cwm.history, usageSample{usedTokens: usedTokens})
	if len(cwm.history) > 100 {
		cwm.history = cwm.history[len(cwm.history)-100:]
	}
	cwm.mu.Unlock()
}

func (cwm *ContextWindowManager) Status() ContextWindowStatus {
	budget := cwm.cm.Budget()
	pct := budget.UsedPercentage()
	level := cwm.warningLevelForPercentage(pct)

	return ContextWindowStatus{
		ContextBudget: budget,
		WarningLevel:  level,
		Advice:        adviceForLevel(level, pct),
	}
}

func (cwm *ContextWindowManager) warningLevelForPercentage(pct float64) WarningLevel {
	warn, caution, trigger, hardBlock := cwm.cm.Thresholds()

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
