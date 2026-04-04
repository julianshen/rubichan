package session

import (
	"sync"
	"time"
)

// Verdict records the outcome of executing a tool call.
type Verdict struct {
	ToolName    string    // Name of the tool (e.g., "shell", "read_file")
	Command     string    // Brief description of what was attempted
	Status      string    // "success", "error", "timeout", "cancelled"
	ErrorReason string    // If Status is not "success", reason for failure
	Timestamp   time.Time // When the verdict was recorded
}

// ToolSummary aggregates verdicts for a single tool.
type ToolSummary struct {
	Total      int
	Successful int
	Failed     int
	Errors     int
}

// VerdictHistory tracks tool execution outcomes across turns.
// It maintains a limited history (most recent N verdicts) to prevent
// unbounded memory growth while preserving enough context for learning.
type VerdictHistory struct {
	mu       sync.RWMutex
	verdicts []Verdict
	maxSize  int
}

// NewVerdictHistory creates an empty verdict history with default max size.
func NewVerdictHistory() *VerdictHistory {
	return &VerdictHistory{
		verdicts: []Verdict{},
		maxSize:  100,
	}
}

// Record adds a verdict to the history, evicting oldest if needed.
func (h *VerdictHistory) Record(v Verdict) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.verdicts = append(h.verdicts, v)
	if len(h.verdicts) > h.maxSize {
		// Evict oldest
		h.verdicts = h.verdicts[len(h.verdicts)-h.maxSize:]
	}
}

// Verdicts returns a copy of the verdict history.
func (h *VerdictHistory) Verdicts() []Verdict {
	h.mu.RLock()
	defer h.mu.RUnlock()

	out := make([]Verdict, len(h.verdicts))
	copy(out, h.verdicts)
	return out
}

// SummaryByTool returns aggregated statistics per tool.
func (h *VerdictHistory) SummaryByTool() map[string]ToolSummary {
	h.mu.RLock()
	defer h.mu.RUnlock()

	summary := make(map[string]ToolSummary)
	for _, v := range h.verdicts {
		s := summary[v.ToolName]
		s.Total++
		switch v.Status {
		case "success":
			s.Successful++
		case "error":
			s.Failed++
			s.Errors++
		default:
			s.Failed++
		}
		summary[v.ToolName] = s
	}
	return summary
}

// Clear removes all verdicts from history.
func (h *VerdictHistory) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.verdicts = nil
}
