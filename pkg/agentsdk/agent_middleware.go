package agentsdk

import (
	"context"
	"fmt"
)

// WithToolMiddlewares registers middlewares that wrap every tool execution.
// The first middleware is the outermost wrapper (see NewPipeline). Nil
// middlewares are ignored.
//
// This is the SDK loop's counterpart to internal/agent.WithToolMiddlewares.
// The internal agent owns a fixed core chain (hooks, checkpoint, verdict) and
// so exposes before/after slots around it; the SDK core has no such chain, so
// registration is a single ordered list.
//
// Middlewares wrap execution only: a tool call denied by the approval flow
// never reaches them, because it is never executed.
func WithToolMiddlewares(middlewares ...Middleware) Option {
	return func(a *Agent) {
		for _, mw := range middlewares {
			if mw != nil {
				a.toolMiddlewares = append(a.toolMiddlewares, mw)
			}
		}
	}
}

// dispatchTool runs one approved tool call through the middleware pipeline,
// whose base handler is the shared execution core (registry lookup with
// did-you-mean suggestions, streaming-aware execution, error wrapping).
//
// It returns the outcome along with the name that actually executed, which a
// middleware may have rewritten — the canonicalization pattern
// internal/toolexec.CanonicalizeToolNameMiddleware implements (write_file →
// file). Callers report that name so emitted events describe what really ran
// rather than the alias the model asked for. If no middleware rewrites it, or
// if one short-circuits before the base handler, it is the original name.
//
// The progress emitter is built inside the base handler rather than passed in:
// it stamps events with the call's identity, so it can only be constructed
// once the effective name is known. sink is the turn's event channel adapter.
//
// The whole dispatch runs behind a recover boundary. Both middlewares and
// tools are third-party code on the turn goroutine, and this loop's goroutine
// is unsupervised, so an escaping panic would kill the host process. Folding
// it into an error outcome instead matches what ToolExecOutcome already
// promises for ordinary failures — a misbehaving tool never aborts the turn —
// and gives this seam the same containment as ContextStrategy and
// BackgroundTask.
func (a *Agent) dispatchTool(ctx context.Context, tc ToolUseBlock, sink func(TurnEvent)) (outcome ToolExecOutcome, executedName string) {
	executedName = tc.Name
	defer func() {
		if r := recover(); r != nil {
			a.logger.Warn("tool dispatch panicked for %s: %v", tc.Name, r)
			outcome = ToolExecOutcome{
				Content: fmt.Sprintf("tool execution error: panic: %v", r),
				IsError: true,
			}
		}
	}()

	base := func(ctx context.Context, call ToolCall) Result {
		// Runs synchronously inside Execute on this goroutine, so recording
		// the effective name here needs no synchronization.
		executedName = call.Name
		emit := MakeToolProgressEmitter(call.ID, call.Name, sink)
		out := ExecuteTool(ctx, a.tools, call.Name, call.Input, emit)
		return Result{
			Content:        out.Content,
			DisplayContent: out.DisplayContent,
			IsError:        out.IsError,
		}
	}

	res := NewPipeline(base, a.toolMiddlewares...).Execute(ctx, ToolCall{
		ID:    tc.ID,
		Name:  tc.Name,
		Input: tc.Input,
	})

	return ToolExecOutcome{
		Content:        res.Content,
		DisplayContent: res.DisplayContent,
		IsError:        res.IsError,
	}, executedName
}
