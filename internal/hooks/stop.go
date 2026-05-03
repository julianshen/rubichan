package hooks

import (
	"context"
	"fmt"
	"sync"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// StopHookResult is the aggregated outcome of running stop hooks.
type StopHookResult struct {
	// PreventContinuation stops the loop entirely.
	PreventContinuation bool
	// BlockingErrors are yielded as meta messages but don't stop.
	BlockingErrors []error
	// Messages to inject into the conversation.
	Messages []string
}

// StopHook is a function that runs after each turn.
// It receives the current hook state and returns a result or error.
type StopHook func(ctx context.Context, state HookState) (*StopHookResult, error)

// HookState provides context to stop hooks about the current turn.
type HookState struct {
	TurnCount    int
	ToolCalls    []string
	ResponseText string
	ExitReason   agentsdk.TurnExitReason
}

// StopHookRegistry manages stop hooks.
type StopHookRegistry struct {
	mu    sync.RWMutex
	hooks []StopHook
}

// NewStopHookRegistry creates an empty registry.
func NewStopHookRegistry() *StopHookRegistry {
	return &StopHookRegistry{}
}

// Register adds a stop hook.
func (r *StopHookRegistry) Register(hook StopHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks = append(r.hooks, hook)
}

// RunStopHooks executes all registered stop hooks and aggregates results.
// Hooks run sequentially; if any hook returns preventContinuation,
// subsequent hooks are skipped, but the preventing hook's Messages and
// BlockingErrors are still collected.
func (r *StopHookRegistry) RunStopHooks(ctx context.Context, state HookState) *StopHookResult {
	r.mu.RLock()
	hooks := make([]StopHook, len(r.hooks))
	copy(hooks, r.hooks)
	r.mu.RUnlock()

	result := &StopHookResult{}

	for _, hook := range hooks {
		if ctx.Err() != nil {
			break
		}

		// Recover from panics so one bad hook cannot crash the agent.
		var hookResult *StopHookResult
		var err error
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					err = fmt.Errorf("hook panicked: %v", rec)
				}
			}()
			hookResult, err = hook(ctx, state)
		}()

		if err != nil {
			result.BlockingErrors = append(result.BlockingErrors, err)
			continue
		}

		if hookResult == nil {
			continue
		}

		// Collect messages and errors from this hook before checking
		// PreventContinuation, so a preventing hook can still inject
		// messages and report errors.
		result.BlockingErrors = append(result.BlockingErrors, hookResult.BlockingErrors...)
		result.Messages = append(result.Messages, hookResult.Messages...)

		if hookResult.PreventContinuation {
			result.PreventContinuation = true
			break
		}
	}

	return result
}
