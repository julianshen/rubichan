package session

import (
	"sync"
	"time"
)

// Verdict status constants
const (
	VerdictStatusSuccess   = "success"
	VerdictStatusError     = "error"
	VerdictStatusTimeout   = "timeout"
	VerdictStatusCancelled = "cancelled"
)

// Verdict records the outcome of executing a tool call.
type Verdict struct {
	ToolName    string    // Name of the tool (e.g., "shell", "read_file")
	Command     string    // Brief description of what was attempted
	Status      string    // VerdictStatusSuccess, VerdictStatusError, VerdictStatusTimeout, VerdictStatusCancelled
	ErrorReason string    // If Status is not a success verdict, reason for failure
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
// It maintains a limited history (most recent N verdicts) using a circular buffer
// to prevent unbounded memory growth while preserving enough context for learning.
type VerdictHistory struct {
	mu       sync.RWMutex
	verdicts []Verdict
	size     int   // Current number of verdicts in buffer
	pos      int   // Write position in circular buffer
	maxSize  int   // Maximum capacity
}

// NewVerdictHistory creates an empty verdict history with default max size of 100.
func NewVerdictHistory() *VerdictHistory {
	return NewVerdictHistoryWithSize(100)
}

// NewVerdictHistoryWithSize creates a verdict history with custom max size.
func NewVerdictHistoryWithSize(maxSize int) *VerdictHistory {
	if maxSize < 1 {
		maxSize = 1
	}
	return &VerdictHistory{
		verdicts: make([]Verdict, maxSize),
		maxSize:  maxSize,
	}
}

// Record adds a verdict to the history, evicting oldest if buffer is full.
func (h *VerdictHistory) Record(v Verdict) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.verdicts[h.pos] = v
	h.pos = (h.pos + 1) % h.maxSize
	if h.size < h.maxSize {
		h.size++
	}
}

// Verdicts returns a copy of the verdict history in chronological order (oldest first).
func (h *VerdictHistory) Verdicts() []Verdict {
	h.mu.RLock()
	defer h.mu.RUnlock()

	out := make([]Verdict, 0, h.size)
	if h.size == 0 {
		return out
	}

	// If buffer is full, start from oldest (at pos). Otherwise start from 0.
	start := 0
	if h.size == h.maxSize {
		start = h.pos
	}

	// Iterate through buffer in circular order
	for i := 0; i < h.size; i++ {
		idx := (start + i) % h.maxSize
		out = append(out, h.verdicts[idx])
	}
	return out
}

// SummaryByTool returns aggregated statistics per tool.
func (h *VerdictHistory) SummaryByTool() map[string]ToolSummary {
	h.mu.RLock()
	defer h.mu.RUnlock()

	summary := make(map[string]ToolSummary)
	if h.size == 0 {
		return summary
	}

	// Iterate through buffer in circular order
	start := 0
	if h.size == h.maxSize {
		start = h.pos
	}

	for i := 0; i < h.size; i++ {
		idx := (start + i) % h.maxSize
		v := h.verdicts[idx]
		s := summary[v.ToolName]
		s.Total++
		switch v.Status {
		case VerdictStatusSuccess:
			s.Successful++
		case VerdictStatusError:
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
	h.size = 0
	h.pos = 0
}
