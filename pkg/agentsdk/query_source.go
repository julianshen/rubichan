package agentsdk

// QuerySource classifies the origin of a request for retry behavior.
// Background tasks should not retry on 529 to avoid amplifying overload.
type QuerySource int

const (
	// QuerySourceForeground is a user-facing query. Retries on 529
	// are appropriate because the user is waiting.
	QuerySourceForeground QuerySource = iota
	// QuerySourceBackground is a background task (summary, classifier,
	// compaction). Fails fast on 529 to avoid amplifying capacity cascades.
	QuerySourceBackground
	// QuerySourceHook is a hook-initiated request. Treated as background
	// unless explicitly marked foreground.
	QuerySourceHook
)

func (s QuerySource) String() string {
	switch s {
	case QuerySourceForeground:
		return "foreground"
	case QuerySourceBackground:
		return "background"
	case QuerySourceHook:
		return "hook"
	default:
		return "unknown"
	}
}

// ShouldRetryOn529 returns whether this source should retry when the
// server returns 529 (overloaded). Only foreground queries retry.
func (s QuerySource) ShouldRetryOn529() bool {
	return s == QuerySourceForeground
}
