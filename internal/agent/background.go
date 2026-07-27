package agent

import (
	"context"
	"time"

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

// startBackgroundTurn dispatches to the shared seam runtime in
// pkg/agentsdk, which both loops call so the per-task recover boundaries
// exist once rather than in two copies.
func (a *Agent) startBackgroundTurn(ctx context.Context, info agentsdk.BackgroundTurnInfo) []func(context.Context) {
	return agentsdk.StartBackgroundTurn(ctx, a.backgroundTasks, info, a.logger)
}

// endBackgroundSession dispatches to the shared seam runtime; see
// agentsdk.EndBackgroundSession.
func (a *Agent) endBackgroundSession() {
	agentsdk.EndBackgroundSession(a.backgroundTasks, a.logger)
}

// sessionMemoryExtractionTimeout bounds one extraction pass. The
// extraction goroutine runs on a context detached from the turn, so
// without a local deadline a hung provider stream would leak it forever.
const sessionMemoryExtractionTimeout = 5 * time.Minute

// sessionMemoryBackgroundTask adapts session-memory extraction onto the
// BackgroundTask seam: each join (after tool execution, including terminal
// tool turns) counts the round and, when the gate opens, spawns the async
// extraction model call. Cancelled turns are skipped — the user aborted —
// but the extraction itself runs on a detached, bounded context: product
// callers cancel the turn context the moment "done" is observed, which
// would otherwise kill the extraction HTTP call mid-flight on exactly the
// terminal turns this task exists to cover.
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
				exCtx, cancel := context.WithTimeout(context.Background(), sessionMemoryExtractionTimeout)
				defer cancel()
				if _, err := a.sessionMemory.Extract(exCtx, msgsCopy, a.provider.Stream, a.conversation.SystemPrompt()); err != nil {
					a.logger.Warn("session memory extraction failed: %v", err)
				}
			}(msgs)
		}
	}
}

func (sessionMemoryBackgroundTask) EndSession(context.Context) {}
