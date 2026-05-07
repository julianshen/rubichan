package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCacheBreakDetectorSnapshot(t *testing.T) {
	d := NewCacheBreakDetector()
	d.Snapshot(1, "system prompt", nil, "claude-3", []int{10})
	// Snapshot is stored internally; no return value to check.
	require.NotNil(t, d)
}

func TestCacheBreakDetectorNoBreak(t *testing.T) {
	d := NewCacheBreakDetector()
	d.Snapshot(1, "system", nil, "model", nil)
	d.RecordUsage(1, 10000) // establish baseline

	d.Snapshot(2, "system", nil, "model", nil)
	r := d.RecordUsage(2, 9800) // 2% drop, below 5% threshold
	require.Nil(t, r)
}

func TestCacheBreakDetectorDetectsBreak(t *testing.T) {
	d := NewCacheBreakDetector()
	d.Snapshot(1, "system", nil, "model", nil)
	d.RecordUsage(1, 10000) // establish baseline

	d.Snapshot(2, "changed system", nil, "model", nil)
	r := d.RecordUsage(2, 1000) // 90% drop
	require.NotNil(t, r)
	require.Equal(t, -9000, r.CacheReadDelta)
	require.Contains(t, r.Diagnosis, "9000 tokens")
}

func TestCacheBreakDetectorSmallDropIgnored(t *testing.T) {
	d := NewCacheBreakDetector()
	d.Snapshot(1, "system", nil, "model", nil)
	d.RecordUsage(1, 10000)

	d.Snapshot(2, "system", nil, "model", nil)
	r := d.RecordUsage(2, 7900) // 21% drop, 2100 tokens > 2000 threshold
	require.NotNil(t, r)
	require.Equal(t, -2100, r.CacheReadDelta)
}

func TestCacheBreakDetectorReports(t *testing.T) {
	d := NewCacheBreakDetector()
	d.Snapshot(1, "s", nil, "m", nil)
	d.RecordUsage(1, 10000)
	d.Snapshot(2, "s2", nil, "m", nil)
	d.RecordUsage(2, 1000)

	require.Len(t, d.Reports(), 1)
	d.Reset()
	require.Len(t, d.Reports(), 0)
}

func TestCacheBreakDetectorFirstTurnNoBreak(t *testing.T) {
	d := NewCacheBreakDetector()
	d.Snapshot(1, "system", nil, "model", nil)
	r := d.RecordUsage(1, 5000) // first turn, no prior baseline
	require.Nil(t, r)
}

func TestCacheBreakDetectorReportCap(t *testing.T) {
	d := NewCacheBreakDetector()
	// Establish baseline
	d.Snapshot(1, "system", nil, "model", nil)
	d.RecordUsage(1, 100000)

	// Generate more than maxCacheBreakReports (100) breaks
	for i := 2; i < 105; i++ {
		d.Snapshot(i, "system", nil, "model", nil)
		d.RecordUsage(i, 1000) // break: 99% drop from previous baseline
		// Reset baseline high for next iteration so each is a break
		d.Snapshot(i+1, "system", nil, "model", nil)
		d.RecordUsage(i+1, 100000)
	}

	reports := d.Reports()
	require.Len(t, reports, maxCacheBreakReports)
}
