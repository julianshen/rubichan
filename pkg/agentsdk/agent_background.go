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

// startBackgroundTurn dispatches to the shared seam runtime; see
// StartBackgroundTurn for the recover-boundary rationale.
func (a *Agent) startBackgroundTurn(ctx context.Context, info BackgroundTurnInfo) []func(context.Context) {
	return StartBackgroundTurn(ctx, a.backgroundTasks, info, a.logger)
}

// endBackgroundSession dispatches to the shared seam runtime; see
// EndBackgroundSession.
func (a *Agent) endBackgroundSession() {
	EndBackgroundSession(a.backgroundTasks, a.logger)
}
