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
// emit is captured by the base handler rather than passed through ToolCall:
// the emitter is bound to this call's identity and the turn's event channel,
// which the pipeline's ToolCall carries no field for.
//
// The whole dispatch runs behind a recover boundary. Both middlewares and
// tools are third-party code on the turn goroutine, and this loop's goroutine
// is unsupervised, so an escaping panic would kill the host process. Folding
// it into an error outcome instead matches what ToolExecOutcome already
// promises for ordinary failures — a misbehaving tool never aborts the turn —
// and gives this seam the same containment as ContextStrategy and
// BackgroundTask.
func (a *Agent) dispatchTool(ctx context.Context, tc ToolUseBlock, emit ToolEventEmitter) (outcome ToolExecOutcome) {
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
	}
}
