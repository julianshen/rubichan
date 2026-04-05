package toolexec

import (
	"context"
	"fmt"

	"github.com/julianshen/rubichan/internal/evaluator"
)

// VerdictMiddleware appends an evaluation Verdict to the tool result Content
// for any tool whose name is in watchTools. This makes the verdict visible to the LLM
// in the conversation history without separate injection logic in the agent loop.
// If pipeline is nil or watchTools is empty, the middleware passes through unchanged.
func VerdictMiddleware(pipeline *evaluator.CheckerPipeline, watchTools ...string) Middleware {
	watch := make(map[string]struct{}, len(watchTools))
	for _, n := range watchTools {
		watch[n] = struct{}{}
	}

	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, tc ToolCall) Result {
			result := next(ctx, tc)

			// Pass through if no pipeline configured or tool not watched
			if pipeline == nil {
				return result
			}
			if _, ok := watch[tc.Name]; !ok {
				return result
			}

			// Run the checker pipeline on the result
			verdict := pipeline.Evaluate(evaluator.ToolOutput{
				ToolName: tc.Name,
				Content:  result.Content,
				IsError:  result.IsError,
			})

			// Append formatted verdict to the result content
			result.Content = fmt.Sprintf("%s\n\n%s", result.Content, evaluator.FormatVerdict(verdict))
			return result
		}
	}
}
