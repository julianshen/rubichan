package integrations

import (
	"context"
	"fmt"
)

// WorkflowInvoker is the subset of skills.Runtime that SkillInvoker needs.
type WorkflowInvoker interface {
	InvokeWorkflow(ctx context.Context, name string, args map[string]any) (map[string]any, error)
}

// SkillInvoker bridges cross-skill invocation for Starlark and Go plugin backends.
type SkillInvoker struct {
	invoker WorkflowInvoker
}

// NewSkillInvoker creates a SkillInvoker.
func NewSkillInvoker(invoker WorkflowInvoker) *SkillInvoker {
	return &SkillInvoker{invoker: invoker}
}

// SetInvoker sets the underlying WorkflowInvoker. This supports deferred wiring
// when the runtime (which implements WorkflowInvoker) is created after the
// SkillInvoker, breaking the circular dependency.
func (s *SkillInvoker) SetInvoker(invoker WorkflowInvoker) {
	s.invoker = invoker
}

// Invoke calls another skill's workflow by name.
func (s *SkillInvoker) Invoke(ctx context.Context, name string, input map[string]any) (map[string]any, error) {
	if s.invoker == nil {
		return nil, fmt.Errorf("skill invoker not configured")
	}
	return s.invoker.InvokeWorkflow(ctx, name, input)
}
