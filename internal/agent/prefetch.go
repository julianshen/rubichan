package agent

import (
	"context"

	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/pkg/agentsdk"
	kg "github.com/julianshen/rubichan/pkg/knowledgegraph"
)

// PrefetchHandle tracks an in-flight async operation with a result.
type PrefetchHandle[T any] struct {
	Done   chan struct{}
	Result T
	Err    error
}

// Consume blocks until the prefetch completes or the context is cancelled.
// Returns the prefetched result or a zero value if the context is cancelled
// before the async operation finishes.
func (h *PrefetchHandle[T]) Consume(ctx context.Context) (T, error) {
	select {
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	case <-h.Done:
		return h.Result, h.Err
	}
}

// PrefetchManager coordinates async loading of memory and skills.
type PrefetchManager struct {
	kgSelector   kg.ContextSelector
	skillRuntime *skills.Runtime
}

// NewPrefetchManager creates a manager with the given dependencies.
// Either dependency may be nil; the corresponding prefetch will be a no-op.
func NewPrefetchManager(kg kg.ContextSelector, sr *skills.Runtime) *PrefetchManager {
	return &PrefetchManager{
		kgSelector:   kg,
		skillRuntime: sr,
	}
}

// StartMemoryPrefetch begins async loading of knowledge graph entities.
// If kgSelector is nil, the handle completes immediately with no result.
func (pm *PrefetchManager) StartMemoryPrefetch(ctx context.Context, query string, budget int) *PrefetchHandle[[]kg.ScoredEntity] {
	handle := &PrefetchHandle[[]kg.ScoredEntity]{Done: make(chan struct{})}

	go func() {
		defer close(handle.Done)
		if pm.kgSelector == nil {
			return
		}

		entities, err := pm.kgSelector.Select(ctx, query, budget)
		handle.Result = entities
		handle.Err = err
	}()

	return handle
}

// prefetchBackgroundTask adapts a PrefetchManager onto the BackgroundTask
// seam: memory and skill prefetches start before the model call, and the
// join — invoked after tool execution — consumes both handles, recording
// prefetched entities against the agent's knowledge selector.
type prefetchBackgroundTask struct {
	agent *Agent
	mgr   *PrefetchManager
}

func (p *prefetchBackgroundTask) StartTurn(ctx context.Context, info agentsdk.BackgroundTurnInfo) func(context.Context) {
	memHandle := p.mgr.StartMemoryPrefetch(ctx, info.UserMessage, info.MemoryBudget)
	skillHandle := p.mgr.StartSkillPrefetch(ctx, p.agent.buildSkillTriggerContext(info.UserMessage))

	return func(ctx context.Context) {
		entities, err := memHandle.Consume(ctx)
		if err != nil {
			p.agent.logger.Warn("memory prefetch failed: %v", err)
		} else if len(entities) > 0 && p.agent.knowledgeSelector != nil {
			if err := p.agent.knowledgeSelector.RecordUsage(ctx, entities); err != nil {
				p.agent.logger.Warn("record knowledge usage failed: %v", err)
			}
		}

		if _, err := skillHandle.Consume(ctx); err != nil {
			p.agent.logger.Warn("skill prefetch failed: %v", err)
		}
	}
}

func (p *prefetchBackgroundTask) EndSession(context.Context) {}

// StartSkillPrefetch begins async evaluation of skill triggers.
// If skillRuntime is nil, the handle completes immediately with no error.
func (pm *PrefetchManager) StartSkillPrefetch(ctx context.Context, triggerCtx skills.TriggerContext) *PrefetchHandle[struct{}] {
	handle := &PrefetchHandle[struct{}]{Done: make(chan struct{})}

	go func() {
		defer close(handle.Done)
		if pm.skillRuntime == nil {
			return
		}

		handle.Err = pm.skillRuntime.EvaluateAndActivate(triggerCtx)
	}()

	return handle
}
