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
