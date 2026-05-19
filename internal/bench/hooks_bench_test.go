package bench

import (
	"context"
	"fmt"
	"testing"

	"github.com/julianshen/rubichan/internal/skills"
)

// noopHandler is a fast hook handler for benchmarking dispatch overhead.
func noopHandler(event skills.HookEvent) (skills.HookResult, error) {
	return skills.HookResult{}, nil
}

func BenchmarkLifecycleManager_Dispatch_1Handler(b *testing.B) {
	lm := skills.NewLifecycleManager()
	lm.Register(skills.HookOnBeforeToolCall, "test", 0, noopHandler)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = lm.Dispatch(skills.HookEvent{
			Phase: skills.HookOnBeforeToolCall,
			Ctx:   context.Background(),
			Data:  map[string]any{"tool_name": "read"},
		})
	}
}

func BenchmarkLifecycleManager_Dispatch_10Handlers(b *testing.B) {
	lm := skills.NewLifecycleManager()
	for i := 0; i < 10; i++ {
		lm.Register(skills.HookOnBeforeToolCall, fmt.Sprintf("test-%d", i), i, noopHandler)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = lm.Dispatch(skills.HookEvent{
			Phase: skills.HookOnBeforeToolCall,
			Ctx:   context.Background(),
			Data:  map[string]any{"tool_name": "read"},
		})
	}
}

func BenchmarkLifecycleManager_Dispatch_ModifyingPhase(b *testing.B) {
	lm := skills.NewLifecycleManager()
	lm.Register(skills.HookOnAfterResponse, "transform", 0, func(event skills.HookEvent) (skills.HookResult, error) {
		return skills.HookResult{Modified: map[string]any{"response": "modified"}}, nil
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = lm.Dispatch(skills.HookEvent{
			Phase: skills.HookOnAfterResponse,
			Ctx:   context.Background(),
			Data:  map[string]any{"response": "original"},
		})
	}
}

func BenchmarkLifecycleManager_Register(b *testing.B) {
	lm := skills.NewLifecycleManager()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lm.Register(skills.HookOnBeforeToolCall, fmt.Sprintf("test-%d", i), i, noopHandler)
	}
}

func BenchmarkLifecycleManager_Unregister(b *testing.B) {
	lm := skills.NewLifecycleManager()
	for i := 0; i < 100; i++ {
		lm.Register(skills.HookOnBeforeToolCall, fmt.Sprintf("test-%d", i), i, noopHandler)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lm.Unregister("test-50")
	}
}
