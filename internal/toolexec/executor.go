package toolexec

import (
	"context"
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/internal/tools"
)

// ToolLookup abstracts the tool registry.
type ToolLookup interface {
	Get(name string) (tools.Tool, bool)
}

// RegistryExecutor creates a HandlerFunc that looks up tools by name
// in the given ToolLookup and executes them. If the tool is not found,
// it returns an error result with a suggestion if a similar tool exists.
// If tool execution fails, the error is wrapped in the result.
func RegistryExecutor(lookup ToolLookup) HandlerFunc {
	return func(ctx context.Context, tc ToolCall) Result {
		tool, ok := lookup.Get(tc.Name)
		if !ok {
			msg := fmt.Sprintf("unknown tool: %s", tc.Name)
			// Try to suggest the closest matching tool name.
			if namer, ok := lookup.(ToolNamer); ok {
				names := namer.Names()
				if suggestion := SuggestToolName(tc.Name, names); suggestion != "" {
					msg = fmt.Sprintf("unknown tool: %s. Did you mean %q? Available tools: %s",
						tc.Name, suggestion, strings.Join(names, ", "))
				}
			}
			return Result{
				Content: msg,
				IsError: true,
			}
		}

		var (
			tr  tools.ToolResult
			err error
		)
		if st, ok := tool.(tools.StreamingTool); ok {
			if emit := ToolEventEmitterFromContext(ctx); emit != nil {
				tr, err = st.ExecuteStream(ctx, tc.Input, emit)
			} else {
				tr, err = tool.Execute(ctx, tc.Input)
			}
		} else {
			tr, err = tool.Execute(ctx, tc.Input)
		}
		if err != nil {
			return Result{
				Content: fmt.Sprintf("tool execution error: %s", err.Error()),
				IsError: true,
			}
		}

		return Result{
			Content:        tr.Content,
			DisplayContent: tr.DisplayContent,
			IsError:        tr.IsError,
		}
	}
}
