package toolexec

import (
	"context"
	"encoding/json"

	"github.com/julianshen/rubichan/internal/skills"
)

// SkillHookAdapter adapts a skills.Runtime to the HookDispatcher interface.
type SkillHookAdapter struct {
	Runtime *skills.Runtime
}

// DispatchBeforeToolCall dispatches a before-tool-call hook via the skill runtime.
// If Runtime is nil, it returns false (no cancellation) with no error.
func (h *SkillHookAdapter) DispatchBeforeToolCall(ctx context.Context, toolName string, input json.RawMessage) (bool, error) {
	if h.Runtime == nil {
		return false, nil
	}
	result, err := h.Runtime.DispatchHook(skills.HookEvent{
		Phase: skills.HookOnBeforeToolCall,
		Data:  map[string]any{"tool_name": toolName, "input": string(input)},
		Ctx:   ctx,
	})
	if err != nil {
		return false, err
	}
	if result != nil && result.Cancel {
		return true, nil
	}
	return false, nil
}

// DispatchAfterToolResult dispatches an after-tool-result hook via the skill runtime.
// If Runtime is nil, it returns nil with no error.
func (h *SkillHookAdapter) DispatchAfterToolResult(ctx context.Context, toolName, content string, isError bool) (map[string]any, error) {
	if h.Runtime == nil {
		return nil, nil
	}
	result, err := h.Runtime.DispatchHook(skills.HookEvent{
		Phase: skills.HookOnAfterToolResult,
		Data:  map[string]any{"tool_name": toolName, "content": content, "is_error": isError},
		Ctx:   ctx,
	})
	if err != nil {
		return nil, err
	}
	if result != nil {
		return result.Modified, nil
	}
	return nil, nil
}

// ResultStoreAdapter adapts an OffloadResult method to the OutputOffloader interface.
// If Offloader is nil, the original content is returned unchanged.
type ResultStoreAdapter struct {
	Offloader interface {
		OffloadResult(toolName, toolUseID, content string) (string, error)
	}
}

// OffloadResult delegates to the underlying Offloader, or returns content
// unchanged if no Offloader is set.
func (r *ResultStoreAdapter) OffloadResult(toolName, toolUseID, content string) (string, error) {
	if r.Offloader == nil {
		return content, nil
	}
	return r.Offloader.OffloadResult(toolName, toolUseID, content)
}
