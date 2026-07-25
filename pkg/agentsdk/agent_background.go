package agentsdk

import "context"

// WithBackgroundTasks registers tasks that run concurrently with the agent
// loop: started before each model call, joined after tool execution, and
// signalled once when the loop exits. Nil tasks are ignored. See
// BackgroundTask.
//
// This is the SDK loop's counterpart to internal/agent.WithBackgroundTasks:
// it lets an out-of-module embedder overlap its own async work with the
// portable core's model latency without depending on any internal/ package.
func WithBackgroundTasks(tasks ...BackgroundTask) Option {
	return func(a *Agent) {
		for _, task := range tasks {
			if task != nil {
				a.backgroundTasks = append(a.backgroundTasks, task)
			}
		}
	}
}

// startBackgroundTurn starts every registered background task for the
// current turn and returns the join functions to invoke after tool
// execution. StartTurn and the joins run on the main turn goroutine, so
// panics are recovered per task: a bad background optimization must not
// abort the foreground turn or starve sibling tasks.
func (a *Agent) startBackgroundTurn(ctx context.Context, info BackgroundTurnInfo) []func(context.Context) {
	var joins []func(context.Context)
	for _, task := range a.backgroundTasks {
		if join := a.startTaskRecovering(ctx, task, info); join != nil {
			joins = append(joins, a.recoveringJoin(join))
		}
	}
	return joins
}

// startTaskRecovering invokes one task's StartTurn behind a recover
// boundary; on panic the task contributes no join for this turn.
func (a *Agent) startTaskRecovering(ctx context.Context, task BackgroundTask, info BackgroundTurnInfo) (join func(context.Context)) {
	defer func() {
		if r := recover(); r != nil {
			a.logger.Warn("background task StartTurn panicked: %v", r)
		}
	}()
	return task.StartTurn(ctx, info)
}

// recoveringJoin wraps a task's join so a panic in it is contained and
// logged instead of aborting the turn.
func (a *Agent) recoveringJoin(join func(context.Context)) func(context.Context) {
	return func(ctx context.Context) {
		defer func() {
			if r := recover(); r != nil {
				a.logger.Warn("background task join panicked: %v", r)
			}
		}()
		join(ctx)
	}
}

// endBackgroundSession signals session end to every registered background
// task. Each task runs on its own goroutine with a fresh context so
// session-end work is not bound to the (likely finished) turn context and
// never blocks the loop's caller. Panics are recovered per task — this is a
// public seam running third-party code on unsupervised goroutines, where an
// unrecovered panic would take down the whole process.
func (a *Agent) endBackgroundSession() {
	for _, task := range a.backgroundTasks {
		go func(t BackgroundTask) {
			defer func() {
				if r := recover(); r != nil {
					a.logger.Warn("background task EndSession panicked: %v", r)
				}
			}()
			t.EndSession(context.Background())
		}(task)
	}
}
