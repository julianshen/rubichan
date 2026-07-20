package agentsdk

import (
	"context"
	"encoding/json"
)

// ToolCall is the input to the tool-execution pipeline.
type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// Result is the output of the tool-execution pipeline.
type Result struct {
	Content        string
	DisplayContent string
	IsError        bool
}

// HandlerFunc executes a tool call and returns a result.
type HandlerFunc func(ctx context.Context, tc ToolCall) Result

// Middleware wraps a HandlerFunc, adding behavior before/after. This is the
// shared tool-execution middleware seam (docs/MODULAR_CORE_REDESIGN.md §4.1):
// checkpointing, security gating, output offloading, and similar concerns
// compose around a base executor without the core knowing they exist.
type Middleware func(next HandlerFunc) HandlerFunc

// Pipeline composes middlewares around a base executor.
type Pipeline struct {
	middlewares []Middleware
	base        HandlerFunc
}

// NewPipeline creates a Pipeline that composes the given middlewares around
// the base handler. The first middleware in the list is the outermost wrapper.
func NewPipeline(base HandlerFunc, middlewares ...Middleware) *Pipeline {
	return &Pipeline{
		base:        base,
		middlewares: middlewares,
	}
}

// Execute runs the pipeline: middlewares wrap the base handler in order
// (first middleware is outermost), then the composed handler is called
// with the given context and tool call.
func (p *Pipeline) Execute(ctx context.Context, tc ToolCall) Result {
	handler := p.base
	// Apply in reverse so that the first middleware is outermost.
	for i := len(p.middlewares) - 1; i >= 0; i-- {
		handler = p.middlewares[i](handler)
	}
	return handler(ctx, tc)
}

// PipelineEventType distinguishes tool progress events from the final
// result on a Pipeline's streaming channel. Named to avoid colliding with
// the unrelated provider-level StreamEvent (types.go).
type PipelineEventType int

const (
	// PipelineProgress carries a ToolEvent during execution.
	PipelineProgress PipelineEventType = iota
	// PipelineFinal carries the completed Result.
	PipelineFinal
)

// PipelineEvent is either a progress event or a final result from the pipeline.
type PipelineEvent struct {
	Type   PipelineEventType
	Event  *ToolEvent // set when Type == PipelineProgress
	Result *Result    // set when Type == PipelineFinal
}

// ExecuteStream runs the pipeline in a goroutine and returns a channel of
// PipelineEvents. Progress events arrive first, followed by a single
// PipelineFinal event. The channel is closed after the final event.
func (p *Pipeline) ExecuteStream(ctx context.Context, tc ToolCall) <-chan PipelineEvent {
	ch := make(chan PipelineEvent, 32)
	go func() {
		defer close(ch)
		// send tries a non-blocking send first so an event is always
		// delivered whenever the buffer has room — even under a cancelled
		// context, where a bare select{ch<-; ctx.Done()} would drop it at
		// random (Go picks a ready arm nondeterministically). Only when the
		// send would actually block do we fall back to the cancellation arm,
		// which keeps a cancelled turn whose consumer stopped reading from
		// wedging this goroutine on a full buffer. The tool itself still runs
		// to completion (Execute is synchronous); only event delivery past a
		// full buffer is abandoned on cancel. Mirrors sendEvent.
		send := func(ev PipelineEvent) {
			select {
			case ch <- ev:
				return
			default:
			}
			select {
			case ch <- ev:
			case <-ctx.Done():
			}
		}
		emit := func(ev ToolEvent) {
			send(PipelineEvent{Type: PipelineProgress, Event: &ev})
		}
		emitCtx := WithToolEventEmitter(ctx, emit)
		result := p.Execute(emitCtx, tc)
		send(PipelineEvent{Type: PipelineFinal, Result: &result})
	}()
	return ch
}

type toolEventEmitterKey struct{}

// WithToolEventEmitter returns a context that carries a tool event emitter.
// This is how a Middleware or the base HandlerFunc reaches the emitter that
// ExecuteStream wired up — HandlerFunc has no other channel for it.
func WithToolEventEmitter(ctx context.Context, emit ToolEventEmitter) context.Context {
	if emit == nil {
		return ctx
	}
	return context.WithValue(ctx, toolEventEmitterKey{}, emit)
}

// ToolEventEmitterFromContext extracts the tool event emitter from the
// context. Returns nil if no emitter was set.
func ToolEventEmitterFromContext(ctx context.Context) ToolEventEmitter {
	emit, _ := ctx.Value(toolEventEmitterKey{}).(ToolEventEmitter)
	return emit
}
