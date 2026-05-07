package agentsdk

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCacheBreakReport(t *testing.T) {
	r := CacheBreakReport{
		TurnNumber:          5,
		ExpectedCacheRead:   10000,
		ActualCacheRead:     2000,
		CacheReadDelta:      -8000,
		Diagnosis:           "system prompt changed",
		SystemPromptChanged: true,
		Timestamp:           time.Now(),
	}
	require.Equal(t, 5, r.TurnNumber)
	require.Equal(t, -8000, r.CacheReadDelta)
	require.True(t, r.SystemPromptChanged)
}
