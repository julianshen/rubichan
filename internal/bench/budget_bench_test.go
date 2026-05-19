package bench

import (
	"fmt"
	"testing"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

func BenchmarkResultBudgetEnforce_Truncate(b *testing.B) {
	enforcer := agent.NewResultBudgetEnforcer(100, nil)
	result := agentsdk.ToolResult{
		Content: "this is a very long result that definitely exceeds the budget of one hundred characters and needs truncation",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = enforcer.Enforce("read", "tu-1", result)
	}
}

func BenchmarkResultBudgetEnforce_WithinBudget(b *testing.B) {
	enforcer := agent.NewResultBudgetEnforcer(1000, nil)
	result := agentsdk.ToolResult{Content: "short"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = enforcer.Enforce("read", fmt.Sprintf("tu-%d", i), result)
	}
}

func BenchmarkResultBudgetEnforce_Offload(b *testing.B) {
	enforcer := agent.NewResultBudgetEnforcer(50, nil)
	result := agentsdk.ToolResult{
		Content: "this result is way too long and should be offloaded to the result store instead of being kept in the conversation",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = enforcer.Enforce("read", fmt.Sprintf("tu-%d", i), result)
	}
}
