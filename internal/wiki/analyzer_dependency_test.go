package wiki

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDependencyAnalyzer_Name(t *testing.T) {
	a := NewDependencyAnalyzer(&mockLLMCompleter{}, t.TempDir())
	assert.Equal(t, "dependencies", a.Name())
}

func TestDependencyAnalyzer_ProducesDesignDecisions(t *testing.T) {
	dir := t.TempDir()
	goModContent := `module github.com/example/myapp

go 1.21

require (
	github.com/some/library v1.2.3
	github.com/another/dep v0.9.0
)
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goModContent), 0o600))

	llm := &mockLLMCompleter{
		responses: map[string]string{
			"go.mod": "## ADR-001: Use Go modules\nDecision: Use Go modules for dependency management.\n\n## ADR-002: Depend on some/library\nDecision: Use some/library for core functionality.",
		},
	}

	a := NewDependencyAnalyzer(llm, dir)
	input := AnalyzerInput{
		ModuleAnalyses: []ModuleAnalysis{
			{Module: "internal/handler", Summary: "HTTP handler module"},
		},
		Architecture: "Layered architecture with provider and tool packages.",
	}

	out, err := a.Analyze(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, out.Documents, 1)

	doc := out.Documents[0]
	assert.Equal(t, "architecture/design-decisions.md", doc.Path)
	assert.Equal(t, "Design Decisions", doc.Title)
	assert.Contains(t, doc.Content, "# Design Decisions")
	assert.Contains(t, doc.Content, "ADR-001")
}

func TestDependencyAnalyzer_NoDependencyFiles(t *testing.T) {
	dir := t.TempDir() // empty directory — no manifest files

	a := NewDependencyAnalyzer(&mockLLMCompleter{}, dir)
	input := AnalyzerInput{
		Architecture: "Some architecture.",
	}

	out, err := a.Analyze(context.Background(), input)
	require.NoError(t, err)
	assert.Empty(t, out.Documents)
	assert.Empty(t, out.Diagrams)
}

func TestDependencyAnalyzer_LLMError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0o600))

	a := NewDependencyAnalyzer(&errorLLMCompleter{err: errors.New("LLM unavailable")}, dir)
	input := AnalyzerInput{
		Architecture: "Some architecture.",
	}

	// LLM errors are non-fatal: should return empty output, no error.
	out, err := a.Analyze(context.Background(), input)
	require.NoError(t, err)
	assert.Empty(t, out.Documents)
}

func TestDependencyAnalyzer_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0o600))

	a := NewDependencyAnalyzer(&cancelingLLMCompleter{}, dir)
	input := AnalyzerInput{
		Architecture: "Some architecture.",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	out, err := a.Analyze(ctx, input)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, out)
}

func TestDependencyAnalyzer_ImplementsInterface(t *testing.T) {
	var _ SpecializedAnalyzer = NewDependencyAnalyzer(&mockLLMCompleter{}, ".")
}

func TestDependencyAnalyzer_MultipleManifests(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"test","dependencies":{"lodash":"^4.0.0"}}`), 0o600))

	llm := &mockLLMCompleter{
		responses: map[string]string{
			"package.json": "## ADR-001: Hybrid Go+Node project\nDecision: Use both Go and Node.js.",
		},
	}

	a := NewDependencyAnalyzer(llm, dir)
	input := AnalyzerInput{
		Architecture: "Hybrid architecture.",
	}

	out, err := a.Analyze(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, out.Documents, 1)
	assert.Equal(t, "architecture/design-decisions.md", out.Documents[0].Path)
}
