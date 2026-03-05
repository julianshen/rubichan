package toolexec

import (
	"context"
	"encoding/json"
	"fmt"
)

// HookDispatcher abstracts skill runtime's hook dispatch.
type HookDispatcher interface {
	DispatchBeforeToolCall(ctx context.Context, toolName string, input json.RawMessage) (cancel bool, err error)
	DispatchAfterToolResult(ctx context.Context, toolName, content string, isError bool) (modified map[string]any, err error)
}

// OutputOffloader abstracts the ResultStore for output management.
type OutputOffloader interface {
	OffloadResult(toolName, toolUseID, content string) (string, error)
}

// HookMiddleware returns a Middleware that dispatches before-tool-call hooks.
// If dispatcher is nil, the middleware passes through without modification.
// If the dispatcher cancels the call, the base handler is not invoked.
func HookMiddleware(dispatcher HookDispatcher) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, tc ToolCall) Result {
			if dispatcher == nil {
				return next(ctx, tc)
			}

			cancel, err := dispatcher.DispatchBeforeToolCall(ctx, tc.Name, tc.Input)
			if err != nil {
				return Result{
					Content: fmt.Sprintf("hook error: %s", err.Error()),
					IsError: true,
				}
			}
			if cancel {
				return Result{
					Content: "tool call cancelled by skill",
					IsError: true,
				}
			}

			return next(ctx, tc)
		}
	}
}

// PostHookMiddleware returns a Middleware that dispatches after-tool-result hooks.
// It calls the next handler first, then dispatches the hook. If the hook returns
// a modified map with a "content" string key, the result's Content is overwritten
// and DisplayContent is cleared. Errors from the hook are silently ignored
// (graceful degradation).
func PostHookMiddleware(dispatcher HookDispatcher) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, tc ToolCall) Result {
			result := next(ctx, tc)

			if dispatcher == nil {
				return result
			}

			modified, err := dispatcher.DispatchAfterToolResult(ctx, tc.Name, result.Content, result.IsError)
			if err != nil {
				// Graceful degradation: errors don't change result.
				return result
			}

			if modified != nil {
				if content, ok := modified["content"].(string); ok {
					result.Content = content
					result.DisplayContent = ""
				}
			}

			return result
		}
	}
}

// OutputManagerMiddleware returns a Middleware that offloads tool results
// via the given OutputOffloader. If offloader is nil or the result is an error,
// the middleware passes through without modification. If offloading fails,
// the original result is preserved.
func OutputManagerMiddleware(offloader OutputOffloader) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, tc ToolCall) Result {
			result := next(ctx, tc)

			if offloader == nil || result.IsError {
				return result
			}

			ref, err := offloader.OffloadResult(tc.Name, tc.ID, result.Content)
			if err != nil {
				// On offloader error, preserve original result.
				return result
			}

			result.Content = ref
			return result
		}
	}
}
