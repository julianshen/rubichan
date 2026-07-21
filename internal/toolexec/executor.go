package toolexec

import (
	"context"

	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// ToolLookup abstracts the tool registry.
type ToolLookup interface {
	Get(name string) (tools.Tool, bool)
}

// ToolNamer provides the list of registered tool names for suggestion.
// The canonical definition lives in pkg/agentsdk.
type ToolNamer = agentsdk.ToolNamer

// CanonicalizeToolNameMiddleware rewrites alias tool names (e.g.
// write_file → file) to the canonical registered name before any other
// middleware runs. The base executor resolves aliases anyway, but
// name-matching middlewares — checkpoint capture, verdict evaluation,
// classification, permission rules — would otherwise see the alias and
// silently skip; register this outermost so every stage matches on one
// name. Unknown names pass through unchanged for the executor's
// did-you-mean handling.
func CanonicalizeToolNameMiddleware(lookup ToolLookup) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, tc ToolCall) Result {
			if tool, ok := lookup.Get(tc.Name); ok && tool.Name() != tc.Name {
				tc.Name = tool.Name()
			}
			return next(ctx, tc)
		}
	}
}

// RegistryExecutor creates a HandlerFunc that dispatches tool calls through
// the shared agentsdk.ExecuteTool core: name lookup (with a suggestion when
// the lookup also implements ToolNamer), streaming-aware execution using
// any context-carried emitter, and error wrapping.
func RegistryExecutor(lookup ToolLookup) HandlerFunc {
	return func(ctx context.Context, tc ToolCall) Result {
		out := agentsdk.ExecuteTool(ctx, lookup, tc.Name, tc.Input, ToolEventEmitterFromContext(ctx))
		return Result{
			Content:        out.Content,
			DisplayContent: out.DisplayContent,
			IsError:        out.IsError,
		}
	}
}
