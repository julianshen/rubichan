package agentsdk

import "context"

// BackgroundTurnInfo carries the per-turn inputs the agent loop offers to
// background tasks when a turn starts.
type BackgroundTurnInfo struct {
	// UserMessage is the user message that started the current loop.
	UserMessage string
	// MemoryBudget is the token budget available for prefetched context
	// (the loop's skill-prompt budget share).
	MemoryBudget int
}

// BackgroundTask runs work concurrently with the agent loop. The loop is
// the caller: it starts tasks before each model call so their async work
// overlaps model latency, joins them after tool execution, and signals
// session end once the loop exits.
//
// StartTurn is invoked before every model call. Implementations kick off
// their async work and return a join function, which the loop invokes
// after tool execution on the same turn; return nil when there is nothing
// to join. On turns that end the loop without tool calls the join is not
// invoked — async work started there still runs, but its results are not
// collected, so joins must not be required for correctness.
//
// EndSession is invoked exactly once when the loop exits, on a goroutine
// off the loop's critical path, with a context independent of the turn's.
type BackgroundTask interface {
	StartTurn(ctx context.Context, info BackgroundTurnInfo) (join func(context.Context))
	EndSession(ctx context.Context)
}
