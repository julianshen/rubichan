package toolexec

import (
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// The tool-execution pipeline types are defined in pkg/agentsdk so external
// embedders and the internal agent share one Middleware seam
// (docs/MODULAR_CORE_REDESIGN.md §4.1, Phase 2 step 1). Aliased here so
// existing code using toolexec.ToolCall etc. compiles unchanged.

// ToolCall is the input to the pipeline.
type ToolCall = agentsdk.ToolCall

// Result is the output of the pipeline.
type Result = agentsdk.Result

// HandlerFunc executes a tool call and returns a result.
type HandlerFunc = agentsdk.HandlerFunc

// Middleware wraps a HandlerFunc, adding behavior before/after.
type Middleware = agentsdk.Middleware

// Pipeline composes middlewares around a base executor.
type Pipeline = agentsdk.Pipeline

// NewPipeline creates a Pipeline that composes the given middlewares around
// the base handler. The first middleware in the list is the outermost wrapper.
func NewPipeline(base HandlerFunc, middlewares ...Middleware) *Pipeline {
	return agentsdk.NewPipeline(base, middlewares...)
}

// StreamEventType distinguishes progress events from the final result.
type StreamEventType = agentsdk.PipelineEventType

const (
	// StreamProgress carries a ToolEvent during execution.
	StreamProgress = agentsdk.PipelineProgress
	// StreamFinal carries the completed Result.
	StreamFinal = agentsdk.PipelineFinal
)

// StreamEvent is either a progress event or a final result from the pipeline.
type StreamEvent = agentsdk.PipelineEvent
