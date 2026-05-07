package agentsdk

import "time"

// CacheBreakReport describes why a prompt cache miss occurred.
type CacheBreakReport struct {
	TurnNumber        int
	ExpectedCacheRead int    // baseline from previous turn
	ActualCacheRead   int    // actual cache read tokens
	CacheReadDelta    int    // negative = cache miss
	Diagnosis         string // human-readable cause
	Timestamp         time.Time
}
