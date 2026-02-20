package wiki

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- mocks ----------

type mockLLMCompleter struct {
	responses map[string]string // substring match -> response
	calls     []string          // recorded prompts
	mu        sync.Mutex
}

func (m *mockLLMCompleter) Complete(ctx context.Context, prompt string) (string, error) {
	m.mu.Lock()
	m.calls = append(m.calls, prompt)
	m.mu.Unlock()
	for key, resp := range m.responses {
		if strings.Contains(prompt, key) {
			return resp, nil
		}
	}
	return "default response", nil
}

type failingLLMCompleter struct {
	failOn string
}

func (f *failingLLMCompleter) Complete(ctx context.Context, prompt string) (string, error) {
	if strings.Contains(prompt, f.failOn) {
		return "", fmt.Errorf("LLM error for %s", f.failOn)
	}
	return "Summary: test\nKeyTypes: none\nPatterns: none\nConcerns: none", nil
}

// ---------- tests ----------

func TestDefaultAnalyzerConfig(t *testing.T) {
	cfg := DefaultAnalyzerConfig()
	assert.Equal(t, 5, cfg.Concurrency)
}

func TestAnalyzeProducesModuleAnalysis(t *testing.T) {
	chunks := []Chunk{
		{
			Module: "internal/handler",
			Files:  []ScannedFile{{Path: "handler.go", Language: "go", Module: "internal/handler"}},
			Source: []byte("package handler\n\nfunc Handle() {}\n"),
		},
	}

	llm := &mockLLMCompleter{
		responses: map[string]string{
			"internal/handler": "Summary: Handles HTTP requests\nKeyTypes: Handler, Router\nPatterns: middleware chain\nConcerns: error handling",
		},
	}

	result, err := Analyze(context.Background(), chunks, llm, DefaultAnalyzerConfig())
	require.NoError(t, err)
	require.Len(t, result.Modules, 1)

	mod := result.Modules[0]
	assert.Equal(t, "internal/handler", mod.Module)
	assert.Contains(t, mod.Summary, "Handles HTTP requests")
	assert.Contains(t, mod.KeyTypes, "Handler")
	assert.Contains(t, mod.Patterns, "middleware")
	assert.Contains(t, mod.Concerns, "error handling")

	// Architecture and Suggestions should also be populated
	assert.NotEmpty(t, result.Architecture)
	assert.NotNil(t, result.Suggestions)
}

func TestAnalyzeConcurrentModules(t *testing.T) {
	chunks := []Chunk{
		{Module: "mod/a", Files: []ScannedFile{{Path: "a.go", Module: "mod/a"}}, Source: []byte("package a")},
		{Module: "mod/b", Files: []ScannedFile{{Path: "b.go", Module: "mod/b"}}, Source: []byte("package b")},
		{Module: "mod/c", Files: []ScannedFile{{Path: "c.go", Module: "mod/c"}}, Source: []byte("package c")},
	}

	llm := &mockLLMCompleter{
		responses: map[string]string{
			"mod/a": "Summary: Module A\nKeyTypes: TypeA\nPatterns: none\nConcerns: none",
			"mod/b": "Summary: Module B\nKeyTypes: TypeB\nPatterns: none\nConcerns: none",
			"mod/c": "Summary: Module C\nKeyTypes: TypeC\nPatterns: none\nConcerns: none",
		},
	}

	result, err := Analyze(context.Background(), chunks, llm, DefaultAnalyzerConfig())
	require.NoError(t, err)
	require.Len(t, result.Modules, 3)

	// Verify all three modules were analyzed (order may vary due to concurrency)
	modules := map[string]bool{}
	for _, m := range result.Modules {
		modules[m.Module] = true
	}
	assert.True(t, modules["mod/a"])
	assert.True(t, modules["mod/b"])
	assert.True(t, modules["mod/c"])
}

func TestAnalyzeEmptyChunks(t *testing.T) {
	llm := &mockLLMCompleter{responses: map[string]string{}}

	result, err := Analyze(context.Background(), nil, llm, DefaultAnalyzerConfig())
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Modules)
	assert.Empty(t, result.Architecture)
	assert.Empty(t, result.KeyAbstractions)
	assert.Empty(t, result.Suggestions)
}

func TestAnalyzeModuleFailureContinues(t *testing.T) {
	chunks := []Chunk{
		{Module: "good", Files: []ScannedFile{{Path: "good.go", Module: "good"}}, Source: []byte("package good")},
		{Module: "bad", Files: []ScannedFile{{Path: "bad.go", Module: "bad"}}, Source: []byte("package bad")},
	}

	llm := &failingLLMCompleter{failOn: "bad"}

	result, err := Analyze(context.Background(), chunks, llm, DefaultAnalyzerConfig())
	require.NoError(t, err)

	// Only the successful module should appear
	require.Len(t, result.Modules, 1)
	assert.Equal(t, "good", result.Modules[0].Module)
}
