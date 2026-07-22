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
// execution. StartTurn and the joins run on the main turn goroutine, so
// panics are recovered per task: a bad background optimization must not
// abort the foreground turn (the outer Turn recover would grade it
// ExitPanic) or starve sibling tasks.
func (a *Agent) startBackgroundTurn(ctx context.Context, info agentsdk.BackgroundTurnInfo) []func(context.Context) {
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
func (a *Agent) startTaskRecovering(ctx context.Context, task agentsdk.BackgroundTask, info agentsdk.BackgroundTurnInfo) (join func(context.Context)) {
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

// sessionMemoryBackgroundTask adapts session-memory extraction onto the
// BackgroundTask seam: each join (after tool execution, including terminal
// tool turns) counts the round and, when the gate opens, spawns the async
// extraction model call. Cancelled turns are skipped — extraction against
// a dead context could only fail.
type sessionMemoryBackgroundTask struct{ agent *Agent }

func (t sessionMemoryBackgroundTask) StartTurn(context.Context, agentsdk.BackgroundTurnInfo) func(context.Context) {
	return func(ctx context.Context) {
		a := t.agent
		if a.sessionMemory == nil || ctx.Err() != nil {
			return
		}
		a.sessionMemory.RecordTurn()
		if a.sessionMemory.ShouldExtract(len(a.conversation.Messages())) {
			msgs := a.conversation.Messages()
			go func(msgsCopy []Message) {
				if _, err := a.sessionMemory.Extract(ctx, msgsCopy, a.provider.Stream, a.conversation.SystemPrompt()); err != nil {
					a.logger.Warn("session memory extraction failed: %v", err)
				}
			}(msgs)
		}
	}
}

func (sessionMemoryBackgroundTask) EndSession(context.Context) {}

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
