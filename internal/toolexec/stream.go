package toolexec

import (
	"context"

	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// WithToolEventEmitter and ToolEventEmitterFromContext are defined in
// pkg/agentsdk (docs/MODULAR_CORE_REDESIGN.md §4.1, Phase 2 step 1) so the
// context-carried emitter is the same mechanism used by
// agentsdk.Pipeline.ExecuteStream. Thin wrappers here so existing code
// using toolexec.WithToolEventEmitter etc. compiles unchanged.

// WithToolEventEmitter returns a context that carries a tool event emitter.
func WithToolEventEmitter(ctx context.Context, emit tools.ToolEventEmitter) context.Context {
	return agentsdk.WithToolEventEmitter(ctx, emit)
}

// ToolEventEmitterFromContext extracts the tool event emitter from the context.
// Returns nil if no emitter was set.
func ToolEventEmitterFromContext(ctx context.Context) tools.ToolEventEmitter {
	return agentsdk.ToolEventEmitterFromContext(ctx)
}
