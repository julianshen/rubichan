package agent

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

const (
	cacheBreakThresholdPct    = 5.0  // % drop triggers diagnosis
	cacheBreakThresholdTokens = 2000 // minimum token drop to trigger
	maxCacheBreakReports      = 100  // cap report accumulation
)

// CacheKeyFingerprint captures cache-key parameters before a model call.
type CacheKeyFingerprint struct {
	SystemPromptHash    string
	ToolsHash           string
	CacheControlHash    string
	Model               string
	TurnNumber          int
	PrevCacheReadTokens int
}

// CacheBreakDetector tracks prompt cache stability across turns.
type CacheBreakDetector struct {
	mu                  sync.Mutex
	lastFingerprint     *CacheKeyFingerprint
	lastCacheReadTokens int
	reports             []agentsdk.CacheBreakReport
}

// NewCacheBreakDetector creates a new detector.
func NewCacheBreakDetector() *CacheBreakDetector {
	return &CacheBreakDetector{}
}

// Snapshot captures the current cache-key state before a model call.
func (d *CacheBreakDetector) Snapshot(turnNumber int, systemPrompt string, tools []agentsdk.ToolDef, model string, cacheBreakpoints []int) {
	// Compute hashes outside the lock to minimize contention.
	fingerprint := &CacheKeyFingerprint{
		SystemPromptHash: hashString(systemPrompt),
		ToolsHash:        hashToolDefs(tools),
		CacheControlHash: hashInts(cacheBreakpoints),
		Model:            model,
		TurnNumber:       turnNumber,
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	fingerprint.PrevCacheReadTokens = d.lastCacheReadTokens
	d.lastFingerprint = fingerprint
}

// RecordUsage compares actual cache read tokens against baseline and diagnoses breaks.
func (d *CacheBreakDetector) RecordUsage(turnNumber, cacheReadTokens int) *agentsdk.CacheBreakReport {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.lastFingerprint == nil {
		d.lastCacheReadTokens = cacheReadTokens
		return nil
	}

	prev := d.lastFingerprint.PrevCacheReadTokens
	if prev <= 0 {
		d.lastCacheReadTokens = cacheReadTokens
		return nil
	}

	delta := cacheReadTokens - prev
	dropPct := float64(-delta) / float64(prev) * 100

	if dropPct < cacheBreakThresholdPct || -delta < cacheBreakThresholdTokens {
		d.lastCacheReadTokens = cacheReadTokens
		return nil
	}

	report := agentsdk.CacheBreakReport{
		TurnNumber:        turnNumber,
		ExpectedCacheRead: prev,
		ActualCacheRead:   cacheReadTokens,
		CacheReadDelta:    delta,
		Diagnosis:         d.diagnose(delta),
		Timestamp:         timeNow(),
	}
	if len(d.reports) >= maxCacheBreakReports {
		d.reports = d.reports[1:] // drop oldest
	}
	d.reports = append(d.reports, report)
	d.lastCacheReadTokens = cacheReadTokens
	return &report
}

// diagnose returns a human-readable explanation for the cache break.
func (d *CacheBreakDetector) diagnose(delta int) string {
	if d.lastFingerprint == nil {
		return "unknown: no prior fingerprint"
	}
	return fmt.Sprintf("cache read dropped by %d tokens (%.1f%%); check system prompt, tools, or cache_control changes",
		-delta, float64(-delta)/float64(d.lastFingerprint.PrevCacheReadTokens)*100)
}

// Reports returns all detected cache break reports.
func (d *CacheBreakDetector) Reports() []agentsdk.CacheBreakReport {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]agentsdk.CacheBreakReport(nil), d.reports...)
}

// Reset clears all state.
func (d *CacheBreakDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastFingerprint = nil
	d.lastCacheReadTokens = 0
	d.reports = d.reports[:0]
}

func hashString(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func hashToolDefs(tools []agentsdk.ToolDef) string {
	h := sha256.New()
	for _, t := range tools {
		h.Write([]byte(t.Name))
		h.Write([]byte("\x00"))
		h.Write([]byte(t.Description))
		h.Write([]byte("\x00"))
		h.Write(t.InputSchema)
		h.Write([]byte("\x00"))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func hashInts(ints []int) string {
	h := sha256.New()
	for _, v := range ints {
		h.Write([]byte(fmt.Sprintf("%d,", v)))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// timeNow is a variable for testability.
var timeNow = func() time.Time {
	return time.Now()
}
