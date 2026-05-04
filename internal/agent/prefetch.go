package agent

import (
	"context"

	"github.com/julianshen/rubichan/internal/skills"
	kg "github.com/julianshen/rubichan/pkg/knowledgegraph"
)

// PrefetchHandle tracks an in-flight async operation with a result.
type PrefetchHandle[T any] struct {
	Done   chan struct{}
	Result T
	Err    error
}

// Consume waits for and returns the prefetch result.
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
func NewPrefetchManager(kg kg.ContextSelector, sr *skills.Runtime) *PrefetchManager {
	return &PrefetchManager{
		kgSelector:   kg,
		skillRuntime: sr,
	}
}

// StartMemoryPrefetch begins async loading of knowledge graph entities.
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

// StartSkillPrefetch begins async evaluation of skill triggers.
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
