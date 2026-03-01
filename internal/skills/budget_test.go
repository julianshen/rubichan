package skills

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Feature 3: Context Budget Management ---

func TestNewContextBudget(t *testing.T) {
	t.Run("default budget has sensible values", func(t *testing.T) {
		b := DefaultContextBudget()
		assert.Equal(t, 8000, b.MaxTotalTokens)
		assert.Equal(t, 2000, b.MaxPerSkillTokens)
	})

	t.Run("custom values", func(t *testing.T) {
		b := ContextBudget{MaxTotalTokens: 4000, MaxPerSkillTokens: 1000}
		assert.Equal(t, 4000, b.MaxTotalTokens)
		assert.Equal(t, 1000, b.MaxPerSkillTokens)
	})
}

func TestContextBudgetSourcePriority(t *testing.T) {
	// Higher value = higher priority for budget allocation.
	assert.Greater(t, sourceBudgetPriority(SourceInline), sourceBudgetPriority(SourceBuiltin))
	assert.Greater(t, sourceBudgetPriority(SourceBuiltin), sourceBudgetPriority(SourceUser))
	assert.Greater(t, sourceBudgetPriority(SourceUser), sourceBudgetPriority(SourceProject))
	assert.Greater(t, sourceBudgetPriority(SourceProject), sourceBudgetPriority(SourceMCP))
	assert.Greater(t, sourceBudgetPriority(SourceMCP), sourceBudgetPriority(Source("unknown")))
}

func TestPromptCollectorBudgetedFragmentsUnderBudget(t *testing.T) {
	pc := NewPromptCollector()
	pc.Add(PromptFragment{
		SkillName:      "skill-a",
		ResolvedPrompt: "Short prompt.",
		Source:         SourceUser,
	})
	pc.Add(PromptFragment{
		SkillName:      "skill-b",
		ResolvedPrompt: "Another short prompt.",
		Source:         SourceProject,
	})

	budget := &ContextBudget{MaxTotalTokens: 10000, MaxPerSkillTokens: 5000}
	result := pc.BudgetedFragments(budget)

	assert.Len(t, result, 2)
	assert.Equal(t, "Short prompt.", result[0].ResolvedPrompt)
	assert.Equal(t, "Another short prompt.", result[1].ResolvedPrompt)
}

func TestPromptCollectorBudgetedFragmentsOverBudget(t *testing.T) {
	pc := NewPromptCollector()

	// High priority skill.
	pc.Add(PromptFragment{
		SkillName:      "high-pri",
		ResolvedPrompt: strings.Repeat("x", 400), // 100 tokens
		Source:         SourceInline,
	})
	// Low priority skill — should be excluded when budget is tight.
	pc.Add(PromptFragment{
		SkillName:      "low-pri",
		ResolvedPrompt: strings.Repeat("y", 400), // 100 tokens
		Source:         SourceMCP,
	})

	// Budget only allows ~100 tokens.
	budget := &ContextBudget{MaxTotalTokens: 100}
	result := pc.BudgetedFragments(budget)

	// Only the high-priority skill should be included.
	require.Len(t, result, 1)
	assert.Equal(t, "high-pri", result[0].SkillName)
}

func TestPromptCollectorBudgetedFragmentsTruncation(t *testing.T) {
	pc := NewPromptCollector()

	// 1200 chars = 300 tokens, but per-skill limit is 100 tokens.
	pc.Add(PromptFragment{
		SkillName:      "verbose-skill",
		ResolvedPrompt: strings.Repeat("z", 1200),
		Source:         SourceUser,
	})

	budget := &ContextBudget{
		MaxTotalTokens:    10000,
		MaxPerSkillTokens: 100,
	}
	result := pc.BudgetedFragments(budget)

	require.Len(t, result, 1)
	// Truncated to 100 tokens * 4 chars = 400 chars.
	assert.Equal(t, 400, len(result[0].ResolvedPrompt))
}

func TestPromptCollectorBudgetedFragmentsPreservesOrder(t *testing.T) {
	pc := NewPromptCollector()

	pc.Add(PromptFragment{
		SkillName:      "project-skill",
		ResolvedPrompt: "Project prompt",
		Source:         SourceProject,
	})
	pc.Add(PromptFragment{
		SkillName:      "inline-skill",
		ResolvedPrompt: "Inline prompt",
		Source:         SourceInline,
	})
	pc.Add(PromptFragment{
		SkillName:      "user-skill",
		ResolvedPrompt: "User prompt",
		Source:         SourceUser,
	})

	budget := &ContextBudget{MaxTotalTokens: 10000}
	result := pc.BudgetedFragments(budget)

	require.Len(t, result, 3)
	// Sorted by priority: inline > user > project.
	assert.Equal(t, "inline-skill", result[0].SkillName)
	assert.Equal(t, "user-skill", result[1].SkillName)
	assert.Equal(t, "project-skill", result[2].SkillName)
}

func TestPromptCollectorBudgetedFragmentsNoBudget(t *testing.T) {
	pc := NewPromptCollector()
	pc.Add(PromptFragment{
		SkillName:      "any-skill",
		ResolvedPrompt: "Any prompt",
		Source:         SourceUser,
	})

	t.Run("nil budget", func(t *testing.T) {
		result := pc.BudgetedFragments(nil)
		require.Len(t, result, 1)
		assert.Equal(t, "Any prompt", result[0].ResolvedPrompt)
	})

	t.Run("zero budget", func(t *testing.T) {
		result := pc.BudgetedFragments(&ContextBudget{})
		require.Len(t, result, 1)
		assert.Equal(t, "Any prompt", result[0].ResolvedPrompt)
	})
}

func TestRuntimeGetBudgetedPromptFragments(t *testing.T) {
	bf := func(manifest SkillManifest, dir string) (SkillBackend, error) {
		return &mockBackend{
			tools: nil,
			hooks: map[HookPhase]HookHandler{},
		}, nil
	}

	rt := newIntegrationRuntime(t, []string{"budget-skill"}, bf)

	m := &SkillManifest{
		Name:        "budget-skill",
		Version:     "1.0.0",
		Description: "A budget-tested prompt skill",
		Types:       []SkillType{SkillTypePrompt},
		Prompt: PromptConfig{
			SystemPromptFile: "inline-prompt-text",
		},
	}
	rt.loader.RegisterBuiltin(m)
	require.NoError(t, rt.Discover(nil))
	require.NoError(t, rt.Activate("budget-skill"))

	// Without budget, all fragments returned.
	fragments := rt.GetBudgetedPromptFragments()
	require.Len(t, fragments, 1)
	assert.Equal(t, "budget-skill", fragments[0].SkillName)

	// With tight budget, still fits.
	budget := DefaultContextBudget()
	rt.SetContextBudget(&budget)
	fragments = rt.GetBudgetedPromptFragments()
	require.Len(t, fragments, 1)
}

func TestContextBudgetEstimateTokens(t *testing.T) {
	assert.Equal(t, 0, estimateTokens(""))
	assert.Equal(t, 1, estimateTokens("hi"))                      // 2 chars -> ceil(2/4) = 1
	assert.Equal(t, 1, estimateTokens("four"))                    // 4 chars -> 1 token
	assert.Equal(t, 3, estimateTokens("hello world!"))            // 12 chars -> 3 tokens
	assert.Equal(t, 25, estimateTokens(strings.Repeat("a", 100))) // 100 chars -> 25 tokens
}

func TestPromptCollectorBudgetedFragmentsPartialFit(t *testing.T) {
	pc := NewPromptCollector()

	// First skill: 200 chars = 50 tokens.
	pc.Add(PromptFragment{
		SkillName:      "fits-fully",
		ResolvedPrompt: strings.Repeat("a", 200),
		Source:         SourceInline,
	})
	// Second skill: 400 chars = 100 tokens, but only ~50 tokens of budget remain.
	pc.Add(PromptFragment{
		SkillName:      "partially-fits",
		ResolvedPrompt: strings.Repeat("b", 400),
		Source:         SourceUser,
	})

	budget := &ContextBudget{MaxTotalTokens: 100}
	result := pc.BudgetedFragments(budget)

	require.Len(t, result, 2)
	// First skill fully included.
	assert.Equal(t, 200, len(result[0].ResolvedPrompt))
	assert.Equal(t, "fits-fully", result[0].SkillName)
	// Second skill truncated to fit remaining 50 tokens = 200 chars.
	assert.Equal(t, "partially-fits", result[1].SkillName)
	assert.Equal(t, 200, len(result[1].ResolvedPrompt))
}
