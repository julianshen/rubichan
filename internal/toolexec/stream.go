package toolexec

import (
	"context"

	"github.com/julianshen/rubichan/internal/tools"
)

type toolEventEmitterKey struct{}

// WithToolEventEmitter returns a context that carries a tool event emitter.
func WithToolEventEmitter(ctx context.Context, emit tools.ToolEventEmitter) context.Context {
	if emit == nil {
		return ctx
	}
	return context.WithValue(ctx, toolEventEmitterKey{}, emit)
}

// ToolEventEmitterFromContext extracts the tool event emitter from the context.
// Returns nil if no emitter was set.
func ToolEventEmitterFromContext(ctx context.Context) tools.ToolEventEmitter {
	emit, _ := ctx.Value(toolEventEmitterKey{}).(tools.ToolEventEmitter)
	return emit
}
