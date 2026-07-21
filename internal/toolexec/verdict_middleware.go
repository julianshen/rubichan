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
	return VerdictMiddlewareFor(pipeline, func(tc ToolCall) bool {
		_, ok := watch[tc.Name]
		return ok
	})
}

// VerdictMiddlewareFor is the predicate form of VerdictMiddleware: results
// are evaluated for any call shouldEvaluate accepts. Callers whose critical
// operations depend on tool input — e.g. the canonical file tool, where
// only write/patch operations are critical — use this instead of exact
// name matching.
func VerdictMiddlewareFor(pipeline *evaluator.CheckerPipeline, shouldEvaluate func(ToolCall) bool) Middleware {
	return VerdictOffloadStage(pipeline, shouldEvaluate, nil)
}

// VerdictOffloadStage fuses verdict evaluation and output offloading into
// one post-result stage. The fusion is deliberate: the two operations have
// a data dependency that middleware wrapper ordering cannot express — the
// verdict must be *evaluated* against the full pre-offload output (a
// 200-char offload preview hides late error patterns), but it must be
// *appended* after offloading so it stays visible in the conversation
// instead of vanishing into the stored blob. As separate middlewares, one
// of those two properties is always lost.
//
// Flow: evaluate verdict on the raw result (when watched) → offload
// oversized non-error content (when an offloader is configured; offload
// failures preserve the original content) → append the formatted verdict
// to whatever content remains. The stored blob holds the raw tool output.
// A nil offloader degrades to plain verdict evaluation.
func VerdictOffloadStage(pipeline *evaluator.CheckerPipeline, shouldEvaluate func(ToolCall) bool, offloader OutputOffloader) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, tc ToolCall) Result {
			result := next(ctx, tc)

			// Evaluate against the full pre-offload output.
			var verdictText string
			if pipeline != nil && shouldEvaluate != nil && shouldEvaluate(tc) {
				verdict := pipeline.Evaluate(evaluator.ToolOutput{
					ToolName: tc.Name,
					Content:  result.Content,
					IsError:  result.IsError,
				})
				verdictText = evaluator.FormatVerdict(verdict)
			}

			// Offload oversized content (same semantics as
			// OutputManagerMiddleware: errors skip offloading, and an
			// offloader failure preserves the original content).
			if offloader != nil && !result.IsError {
				if ref, err := offloader.OffloadResult(tc.Name, tc.ID, result.Content); err == nil {
					result.Content = ref
				}
			}

			// Append the verdict after offloading so it stays visible.
			if verdictText != "" {
				result.Content = fmt.Sprintf("%s\n\n%s", result.Content, verdictText)
			}
			return result
		}
	}
}
