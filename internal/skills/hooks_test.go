package skills

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterAndDispatchHook(t *testing.T) {
	lm := NewLifecycleManager()

	called := false
	handler := func(event HookEvent) (HookResult, error) {
		called = true
		return HookResult{}, nil
	}

	lm.Register(HookOnActivate, "test-skill", 10, handler)

	results, err := lm.Dispatch(HookEvent{
		Phase:     HookOnActivate,
		SkillName: "",
		Data:      map[string]any{"key": "value"},
		Ctx:       context.Background(),
	})
	require.NoError(t, err)
	assert.True(t, called, "handler should have been called")
	assert.NotNil(t, results)
}

func TestDispatchHookMultipleSkills(t *testing.T) {
	lm := NewLifecycleManager()

	var callOrder []string

	handlerA := func(event HookEvent) (HookResult, error) {
		callOrder = append(callOrder, "A")
		return HookResult{}, nil
	}
	handlerB := func(event HookEvent) (HookResult, error) {
		callOrder = append(callOrder, "B")
		return HookResult{}, nil
	}
	handlerC := func(event HookEvent) (HookResult, error) {
		callOrder = append(callOrder, "C")
		return HookResult{}, nil
	}

	// Register with different priorities: lower number = higher priority.
	// B has priority 20 (project), A has priority 0 (builtin), C has priority 10 (user).
	lm.Register(HookOnConversationStart, "skill-b", PriorityProject, handlerB)
	lm.Register(HookOnConversationStart, "skill-a", PriorityBuiltin, handlerA)
	lm.Register(HookOnConversationStart, "skill-c", PriorityUser, handlerC)

	_, err := lm.Dispatch(HookEvent{
		Phase: HookOnConversationStart,
		Ctx:   context.Background(),
	})
	require.NoError(t, err)

	// Should run in priority order: A (0), C (10), B (20).
	assert.Equal(t, []string{"A", "C", "B"}, callOrder)
}

func TestDispatchHookNoHandlers(t *testing.T) {
	lm := NewLifecycleManager()

	results, err := lm.Dispatch(HookEvent{
		Phase: HookOnActivate,
		Ctx:   context.Background(),
	})
	require.NoError(t, err)
	assert.Nil(t, results, "dispatch with no handlers should return nil result")
}

func TestBeforeToolCallCancel(t *testing.T) {
	lm := NewLifecycleManager()

	// First handler cancels the tool call.
	cancelHandler := func(event HookEvent) (HookResult, error) {
		return HookResult{Cancel: true}, nil
	}

	// Second handler should not run because the first cancelled.
	secondCalled := false
	secondHandler := func(event HookEvent) (HookResult, error) {
		secondCalled = true
		return HookResult{}, nil
	}

	lm.Register(HookOnBeforeToolCall, "cancel-skill", PriorityBuiltin, cancelHandler)
	lm.Register(HookOnBeforeToolCall, "second-skill", PriorityUser, secondHandler)

	results, err := lm.Dispatch(HookEvent{
		Phase: HookOnBeforeToolCall,
		Data:  map[string]any{"tool": "shell"},
		Ctx:   context.Background(),
	})
	require.NoError(t, err)
	require.NotNil(t, results)
	assert.True(t, results.Cancel, "result should signal cancellation")
	assert.False(t, secondCalled, "second handler should not run after cancellation")
}

func TestAfterToolResultModify(t *testing.T) {
	lm := NewLifecycleManager()

	// Handler modifies the tool result content.
	modifyHandler := func(event HookEvent) (HookResult, error) {
		return HookResult{
			Modified: map[string]any{
				"result": "modified-output",
			},
		}, nil
	}

	// Second handler chains on the modified data.
	chainHandler := func(event HookEvent) (HookResult, error) {
		// Should see the modified data from the first handler.
		prev := event.Data["result"]
		return HookResult{
			Modified: map[string]any{
				"result":  prev,
				"chained": true,
			},
		}, nil
	}

	lm.Register(HookOnAfterToolResult, "modifier", PriorityBuiltin, modifyHandler)
	lm.Register(HookOnAfterToolResult, "chainer", PriorityUser, chainHandler)

	results, err := lm.Dispatch(HookEvent{
		Phase: HookOnAfterToolResult,
		Data:  map[string]any{"result": "original-output"},
		Ctx:   context.Background(),
	})
	require.NoError(t, err)
	require.NotNil(t, results)
	assert.Equal(t, "modified-output", results.Modified["result"])
	assert.Equal(t, true, results.Modified["chained"])
}

func TestBeforePromptBuildInject(t *testing.T) {
	lm := NewLifecycleManager()

	// Handler injects a prompt fragment.
	injectHandler := func(event HookEvent) (HookResult, error) {
		return HookResult{
			Modified: map[string]any{
				"prompt_fragment": "You are a Go expert.",
			},
		}, nil
	}

	lm.Register(HookOnBeforePromptBuild, "prompt-skill", PriorityBuiltin, injectHandler)

	results, err := lm.Dispatch(HookEvent{
		Phase: HookOnBeforePromptBuild,
		Data:  map[string]any{},
		Ctx:   context.Background(),
	})
	require.NoError(t, err)
	require.NotNil(t, results)
	assert.Equal(t, "You are a Go expert.", results.Modified["prompt_fragment"])
}

func TestUnregisterBySkillName(t *testing.T) {
	lm := NewLifecycleManager()

	called := false
	handler := func(event HookEvent) (HookResult, error) {
		called = true
		return HookResult{}, nil
	}

	lm.Register(HookOnActivate, "removable-skill", PriorityUser, handler)
	lm.Register(HookOnDeactivate, "removable-skill", PriorityUser, handler)

	// Unregister removes from all phases.
	lm.Unregister("removable-skill")

	result, err := lm.Dispatch(HookEvent{
		Phase: HookOnActivate,
		Ctx:   context.Background(),
	})
	require.NoError(t, err)
	assert.Nil(t, result, "should return nil after unregister removes all handlers")
	assert.False(t, called, "handler should not be called after unregister")

	result, err = lm.Dispatch(HookEvent{
		Phase: HookOnDeactivate,
		Ctx:   context.Background(),
	})
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestUnregisterPreservesOtherSkills(t *testing.T) {
	lm := NewLifecycleManager()

	var calledSkills []string

	lm.Register(HookOnActivate, "keep-skill", PriorityBuiltin, func(event HookEvent) (HookResult, error) {
		calledSkills = append(calledSkills, "keep-skill")
		return HookResult{}, nil
	})
	lm.Register(HookOnActivate, "remove-skill", PriorityUser, func(event HookEvent) (HookResult, error) {
		calledSkills = append(calledSkills, "remove-skill")
		return HookResult{}, nil
	})

	lm.Unregister("remove-skill")

	_, err := lm.Dispatch(HookEvent{
		Phase: HookOnActivate,
		Ctx:   context.Background(),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"keep-skill"}, calledSkills)
}

func TestDispatchHandlerError(t *testing.T) {
	lm := NewLifecycleManager()

	lm.Register(HookOnActivate, "error-skill", PriorityBuiltin, func(event HookEvent) (HookResult, error) {
		return HookResult{}, fmt.Errorf("handler failed")
	})

	result, err := lm.Dispatch(HookEvent{
		Phase: HookOnActivate,
		Ctx:   context.Background(),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error-skill")
	assert.Contains(t, err.Error(), "handler failed")
	assert.Nil(t, result)
}

func TestModifyingPhaseWithNilData(t *testing.T) {
	lm := NewLifecycleManager()

	lm.Register(HookOnAfterToolResult, "modifier", PriorityBuiltin, func(event HookEvent) (HookResult, error) {
		return HookResult{
			Modified: map[string]any{"key": "value"},
		}, nil
	})

	// Dispatch with nil Data to exercise the nil-data branch.
	result, err := lm.Dispatch(HookEvent{
		Phase: HookOnAfterToolResult,
		Ctx:   context.Background(),
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "value", result.Modified["key"])
}
