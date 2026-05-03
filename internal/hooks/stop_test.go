package hooks

import (
	"context"
	"fmt"
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStopHookRegistry(t *testing.T) {
	r := NewStopHookRegistry()

	// Hook that prevents continuation
	r.Register(func(ctx context.Context, state HookState) (*StopHookResult, error) {
		return &StopHookResult{PreventContinuation: true}, nil
	})

	// This hook should not run
	r.Register(func(ctx context.Context, state HookState) (*StopHookResult, error) {
		t.Fatal("should not run after preventContinuation")
		return nil, nil
	})

	result := r.RunStopHooks(context.Background(), HookState{})
	require.True(t, result.PreventContinuation)
}

func TestStopHookRegistry_BlockingErrors(t *testing.T) {
	r := NewStopHookRegistry()

	r.Register(func(ctx context.Context, state HookState) (*StopHookResult, error) {
		return nil, fmt.Errorf("hook error 1")
	})

	r.Register(func(ctx context.Context, state HookState) (*StopHookResult, error) {
		return nil, fmt.Errorf("hook error 2")
	})

	result := r.RunStopHooks(context.Background(), HookState{})
	require.False(t, result.PreventContinuation)
	require.Len(t, result.BlockingErrors, 2)
	assert.Contains(t, result.BlockingErrors[0].Error(), "hook error 1")
	assert.Contains(t, result.BlockingErrors[1].Error(), "hook error 2")
}

func TestStopHookRegistry_Messages(t *testing.T) {
	r := NewStopHookRegistry()

	r.Register(func(ctx context.Context, state HookState) (*StopHookResult, error) {
		return &StopHookResult{Messages: []string{"msg1", "msg2"}}, nil
	})

	r.Register(func(ctx context.Context, state HookState) (*StopHookResult, error) {
		return &StopHookResult{Messages: []string{"msg3"}}, nil
	})

	result := r.RunStopHooks(context.Background(), HookState{})
	require.False(t, result.PreventContinuation)
	require.Len(t, result.Messages, 3)
	assert.Equal(t, "msg1", result.Messages[0])
	assert.Equal(t, "msg2", result.Messages[1])
	assert.Equal(t, "msg3", result.Messages[2])
}

func TestStopHookRegistry_ContextCancelled(t *testing.T) {
	r := NewStopHookRegistry()

	called := false
	r.Register(func(ctx context.Context, state HookState) (*StopHookResult, error) {
		called = true
		return &StopHookResult{}, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := r.RunStopHooks(ctx, HookState{})
	require.False(t, result.PreventContinuation)
	require.False(t, called, "hook should not run when context is cancelled")
	require.Empty(t, result.Messages)
	require.Empty(t, result.BlockingErrors)
}

func TestStopHookRegistry_PreventContinuationCollectsMessages(t *testing.T) {
	r := NewStopHookRegistry()

	r.Register(func(ctx context.Context, state HookState) (*StopHookResult, error) {
		return &StopHookResult{
			PreventContinuation: true,
			Messages:            []string{"injected"},
			BlockingErrors:      []error{fmt.Errorf("warning")},
		}, nil
	})

	r.Register(func(ctx context.Context, state HookState) (*StopHookResult, error) {
		t.Fatal("should not run after preventContinuation")
		return nil, nil
	})

	result := r.RunStopHooks(context.Background(), HookState{})
	require.True(t, result.PreventContinuation)
	require.Len(t, result.Messages, 1)
	assert.Equal(t, "injected", result.Messages[0])
	require.Len(t, result.BlockingErrors, 1)
	assert.Contains(t, result.BlockingErrors[0].Error(), "warning")
}

func TestStopHookRegistry_PanicRecovery(t *testing.T) {
	r := NewStopHookRegistry()

	r.Register(func(ctx context.Context, state HookState) (*StopHookResult, error) {
		panic("hook panic")
	})

	r.Register(func(ctx context.Context, state HookState) (*StopHookResult, error) {
		return &StopHookResult{Messages: []string{"after panic"}}, nil
	})

	result := r.RunStopHooks(context.Background(), HookState{})
	require.False(t, result.PreventContinuation)
	require.Len(t, result.BlockingErrors, 1)
	assert.Contains(t, result.BlockingErrors[0].Error(), "hook panicked")
	require.Len(t, result.Messages, 1)
	assert.Equal(t, "after panic", result.Messages[0])
}

func TestStopHookRegistry_MultiplePreventContinuation(t *testing.T) {
	r := NewStopHookRegistry()

	callOrder := []int{}
	r.Register(func(ctx context.Context, state HookState) (*StopHookResult, error) {
		callOrder = append(callOrder, 1)
		return &StopHookResult{Messages: []string{"msg1"}}, nil
	})
	r.Register(func(ctx context.Context, state HookState) (*StopHookResult, error) {
		callOrder = append(callOrder, 2)
		return &StopHookResult{PreventContinuation: true, Messages: []string{"msg2"}}, nil
	})
	r.Register(func(ctx context.Context, state HookState) (*StopHookResult, error) {
		callOrder = append(callOrder, 3)
		return &StopHookResult{Messages: []string{"msg3"}}, nil
	})

	result := r.RunStopHooks(context.Background(), HookState{})
	require.True(t, result.PreventContinuation)
	assert.Equal(t, []int{1, 2}, callOrder, "only first two hooks should run")
	require.Len(t, result.Messages, 2)
	assert.Equal(t, "msg1", result.Messages[0])
	assert.Equal(t, "msg2", result.Messages[1])
}

func TestStopHookRegistry_NilResult(t *testing.T) {
	r := NewStopHookRegistry()

	r.Register(func(ctx context.Context, state HookState) (*StopHookResult, error) {
		return nil, nil
	})

	result := r.RunStopHooks(context.Background(), HookState{})
	require.False(t, result.PreventContinuation)
	require.Empty(t, result.Messages)
	require.Empty(t, result.BlockingErrors)
}

func TestStopHookRegistry_HookState(t *testing.T) {
	r := NewStopHookRegistry()

	var receivedState HookState
	r.Register(func(ctx context.Context, state HookState) (*StopHookResult, error) {
		receivedState = state
		return &StopHookResult{}, nil
	})

	state := HookState{
		TurnCount:    5,
		ToolCalls:    []string{"read_file", "grep"},
		ResponseText: "hello",
		ExitReason:   agentsdk.ExitCompleted,
	}

	r.RunStopHooks(context.Background(), state)
	assert.Equal(t, 5, receivedState.TurnCount)
	assert.Equal(t, []string{"read_file", "grep"}, receivedState.ToolCalls)
	assert.Equal(t, "hello", receivedState.ResponseText)
	assert.Equal(t, agentsdk.ExitCompleted, receivedState.ExitReason)
}

func TestStopHookRegistry_Empty(t *testing.T) {
	r := NewStopHookRegistry()
	result := r.RunStopHooks(context.Background(), HookState{})
	require.False(t, result.PreventContinuation)
	require.Empty(t, result.Messages)
	require.Empty(t, result.BlockingErrors)
}
