package agent

import (
	"context"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// WithBackgroundTasks registers tasks that run concurrently with the agent
// loop: started before each model call, joined after tool execution, and
// signalled once when the loop exits. Nil tasks are ignored. See
// agentsdk.BackgroundTask.
func WithBackgroundTasks(tasks ...agentsdk.BackgroundTask) AgentOption {
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
// execution.
func (a *Agent) startBackgroundTurn(ctx context.Context, info agentsdk.BackgroundTurnInfo) []func(context.Context) {
	var joins []func(context.Context)
	for _, task := range a.backgroundTasks {
		if join := task.StartTurn(ctx, info); join != nil {
			joins = append(joins, join)
		}
	}
	return joins
}

// endBackgroundSession signals session end to every registered background
// task. Each task runs on its own goroutine with a fresh context so
// session-end work is not bound to the (likely finished) turn context and
// never blocks the loop's caller. Panics are recovered per task — this is
// a public seam running third-party code on unsupervised goroutines, where
// an unrecovered panic would take down the whole process.
func (a *Agent) endBackgroundSession() {
	for _, task := range a.backgroundTasks {
		go func(t agentsdk.BackgroundTask) {
			defer func() {
				if r := recover(); r != nil {
					a.logger.Warn("background task EndSession panicked: %v", r)
				}
			}()
			t.EndSession(context.Background())
		}(task)
	}
}
