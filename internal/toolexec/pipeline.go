package toolexec

import (
	"context"
	"encoding/json"
)

// ToolCall is the input to the pipeline.
type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// Result is the output of the pipeline.
type Result struct {
	Content        string
	DisplayContent string
	IsError        bool
}

// HandlerFunc executes a tool call and returns a result.
type HandlerFunc func(ctx context.Context, tc ToolCall) Result

// Middleware wraps a HandlerFunc, adding behavior before/after.
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
