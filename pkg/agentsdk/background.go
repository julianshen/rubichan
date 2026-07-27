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

// StartBackgroundTurn starts every task for the current turn and returns the
// join functions to invoke after tool execution.
//
// StartTurn and the joins run on the caller's turn goroutine, so panics are
// recovered per task: a bad background optimization must not abort the
// foreground turn or starve sibling tasks. A task whose StartTurn panics
// contributes no join for this turn.
//
// This is the seam's dispatch, shared by both agent loops rather than
// reimplemented in each. The recover boundaries are the reason: defensive
// code that exists in two copies gains fixes in one, which is how the
// cancellation handling in tool execution came to differ between the cores.
func StartBackgroundTurn(ctx context.Context, tasks []BackgroundTask, info BackgroundTurnInfo, logger Logger) []func(context.Context) {
	var joins []func(context.Context)
	for _, task := range tasks {
		if join := startTaskRecovering(ctx, task, info, logger); join != nil {
			joins = append(joins, recoveringJoin(join, logger))
		}
	}
	return joins
}

// startTaskRecovering invokes one task's StartTurn behind a recover boundary.
func startTaskRecovering(ctx context.Context, task BackgroundTask, info BackgroundTurnInfo, logger Logger) (join func(context.Context)) {
	defer func() {
		if r := recover(); r != nil {
			logger.Warn("background task StartTurn panicked: %v", r)
		}
	}()
	return task.StartTurn(ctx, info)
}

// recoveringJoin wraps a task's join so a panic in it is contained and logged
// instead of aborting the turn.
func recoveringJoin(join func(context.Context), logger Logger) func(context.Context) {
	return func(ctx context.Context) {
		defer func() {
			if r := recover(); r != nil {
				logger.Warn("background task join panicked: %v", r)
			}
		}()
		join(ctx)
	}
}

// EndBackgroundSession signals session end to every task. Each runs on its own
// goroutine with a fresh context, so session-end work is not bound to the
// (likely finished) turn context and never blocks the loop's caller. Panics
// are recovered per task — this is a public seam running third-party code on
// unsupervised goroutines, where an unrecovered panic would take down the
// whole process.
func EndBackgroundSession(tasks []BackgroundTask, logger Logger) {
	for _, task := range tasks {
		go func(t BackgroundTask) {
			defer func() {
				if r := recover(); r != nil {
					logger.Warn("background task EndSession panicked: %v", r)
				}
			}()
			t.EndSession(context.Background())
		}(task)
	}
}
