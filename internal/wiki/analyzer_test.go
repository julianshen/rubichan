package wiki

import (
	"bytes"
	"context"
	"fmt"
	"log"
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

func TestAnalyzeBase_ProducesModulesAndArchitecture(t *testing.T) {
	chunks := []Chunk{
		{
			Module: "internal/auth",
			Files:  []ScannedFile{{Path: "auth.go", Language: "go", Module: "internal/auth"}},
			Source: []byte("package auth\n\nfunc Authenticate() bool { return true }\n"),
		},
	}

	llm := &mockLLMCompleter{
		responses: map[string]string{
			"internal/auth": "Summary: Auth module\nKeyTypes: AuthHandler\nPatterns: token-based\nConcerns: none",
			"architecture":  "Architecture: Simple layered\nKeyAbstractions: AuthHandler",
			"improvements":  "Use OAuth2\nAdd rate limiting",
		},
	}

	result, err := AnalyzeBase(context.Background(), chunks, llm, DefaultAnalyzerConfig())
	require.NoError(t, err)
	require.Len(t, result.Modules, 1)
	assert.Equal(t, "internal/auth", result.Modules[0].Module)
	assert.NotEmpty(t, result.Architecture)
	// Suggestions must be nil/empty — AnalyzeBase does not run generateSuggestions.
	assert.Empty(t, result.Suggestions)
}

func TestRunSpecializedAnalyzers_Concurrent(t *testing.T) {
	docs1 := []Document{{Path: "a1.md", Title: "A1", Content: "content1"}}
	diags1 := []Diagram{{Title: "D1", Type: "architecture", Content: "graph TD"}}
	docs2 := []Document{{Path: "a2.md", Title: "A2", Content: "content2"}}
	diags2 := []Diagram{{Title: "D2", Type: "sequence", Content: "sequenceDiagram"}}

	analyzers := []SpecializedAnalyzer{
		newFuncAnalyzer("a1", docs1, diags1, nil),
		newFuncAnalyzer("a2", docs2, diags2, nil),
	}

	input := AnalyzerInput{Architecture: "test arch"}
	docs, diags, err := RunSpecializedAnalyzers(context.Background(), analyzers, input)
	require.NoError(t, err)
	assert.Len(t, docs, 2)
	assert.Len(t, diags, 2)

	// Verify both analyzers contributed.
	paths := map[string]bool{}
	for _, d := range docs {
		paths[d.Path] = true
	}
	assert.True(t, paths["a1.md"])
	assert.True(t, paths["a2.md"])
}

func TestRunSpecializedAnalyzers_NonFatalError(t *testing.T) {
	docs1 := []Document{{Path: "ok.md", Title: "OK", Content: "fine"}}
	analyzers := []SpecializedAnalyzer{
		newFuncAnalyzer("good", docs1, nil, nil),
		newFuncAnalyzer("bad", nil, nil, fmt.Errorf("analyzer failed")),
	}

	input := AnalyzerInput{Architecture: "test arch"}
	docs, diags, err := RunSpecializedAnalyzers(context.Background(), analyzers, input)
	require.NoError(t, err)
	assert.Len(t, docs, 1)
	assert.Equal(t, "ok.md", docs[0].Path)
	assert.Empty(t, diags)
}

func TestAnalyze_CompatWrapper(t *testing.T) {
	chunks := []Chunk{
		{
			Module: "internal/service",
			Files:  []ScannedFile{{Path: "svc.go", Language: "go", Module: "internal/service"}},
			Source: []byte("package service\n\nfunc Serve() {}\n"),
		},
	}

	llm := &mockLLMCompleter{
		responses: map[string]string{
			"internal/service": "Summary: Service layer\nKeyTypes: Service\nPatterns: none\nConcerns: none",
			"Architecture":     "Architecture: Single-layer\nKeyAbstractions: Service",
			"improvements":     "Refactor to microservices\nAdd tracing",
		},
	}

	result, err := Analyze(context.Background(), chunks, llm, DefaultAnalyzerConfig())
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Modules, 1)
	assert.NotEmpty(t, result.Architecture)
	// Backward compat: Analyze() must still populate Suggestions.
	assert.NotNil(t, result.Suggestions)
}

// newFuncAnalyzer creates a SpecializedAnalyzer whose Analyze method returns the
// given docs/diags/err. Used for in-test stub construction without package-level types.
func newFuncAnalyzer(name string, docs []Document, diags []Diagram, analyzeErr error) SpecializedAnalyzer {
	return &funcAnalyzer{analyzeName: name, docs: docs, diags: diags, err: analyzeErr}
}

type funcAnalyzer struct {
	analyzeName string
	docs        []Document
	diags       []Diagram
	err         error
}

func (f *funcAnalyzer) Name() string { return f.analyzeName }

func (f *funcAnalyzer) Analyze(_ context.Context, _ AnalyzerInput) (*AnalyzerOutput, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &AnalyzerOutput{Documents: f.docs, Diagrams: f.diags}, nil
}

func TestAnalyzeCancellationReturnsContextErrorWithoutWarnings(t *testing.T) {
	chunks := []Chunk{
		{Module: "mod/a", Files: []ScannedFile{{Path: "a.go", Module: "mod/a"}}, Source: []byte("package a")},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// This test redirects the package-level logger and must stay non-parallel.
	var logs bytes.Buffer
	origWriter := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(origWriter)

	result, err := Analyze(ctx, chunks, &cancelingLLMCompleter{}, DefaultAnalyzerConfig())
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, result)
	assert.NotContains(t, logs.String(), "WARNING:")
}
