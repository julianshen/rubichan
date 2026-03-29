package wiki

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLLM is already defined in analyzer_test.go as mockLLMCompleter.
// errorLLMCompleter is a simple always-failing completer for suggestion tests.
type errorLLMCompleter struct {
	err error
}

func (e *errorLLMCompleter) Complete(_ context.Context, _ string) (string, error) {
	return "", e.err
}

func TestSuggestionAnalyzer_Name(t *testing.T) {
	a := NewSuggestionAnalyzer(&mockLLMCompleter{})
	assert.Equal(t, "suggestions", a.Name())
}

func TestSuggestionAnalyzer_ProducesDocument(t *testing.T) {
	llm := &mockLLMCompleter{
		responses: map[string]string{
			"suggest": "Use dependency injection\nAdd more tests\nImprove error handling",
		},
	}
	a := NewSuggestionAnalyzer(llm)

	input := AnalyzerInput{
		Architecture: "Layered architecture with provider, tool, and agent packages.",
	}

	out, err := a.Analyze(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, out.Documents, 1)

	doc := out.Documents[0]
	assert.Equal(t, "suggestions/improvements.md", doc.Path)
	assert.Equal(t, "Improvement Suggestions", doc.Title)
	assert.Contains(t, doc.Content, "# Improvement Suggestions")
	assert.Contains(t, doc.Content, "Use dependency injection")
}

func TestSuggestionAnalyzer_EmptyArchitecture(t *testing.T) {
	a := NewSuggestionAnalyzer(&mockLLMCompleter{})

	out, err := a.Analyze(context.Background(), AnalyzerInput{Architecture: ""})
	require.NoError(t, err)
	assert.Empty(t, out.Documents)
	assert.Empty(t, out.Diagrams)
}

func TestSuggestionAnalyzer_LLMError(t *testing.T) {
	a := NewSuggestionAnalyzer(&errorLLMCompleter{err: errors.New("LLM unavailable")})

	input := AnalyzerInput{
		Architecture: "Some architecture description.",
	}

	// LLM errors are non-fatal: should return empty output, no error.
	out, err := a.Analyze(context.Background(), input)
	require.NoError(t, err)
	assert.Empty(t, out.Documents)
}

func TestSuggestionAnalyzer_EmptyLLMResponse(t *testing.T) {
	llm := &mockLLMCompleter{
		responses: map[string]string{
			"suggest": "   ", // whitespace only
		},
	}
	a := NewSuggestionAnalyzer(llm)

	input := AnalyzerInput{
		Architecture: "Some architecture description.",
	}

	out, err := a.Analyze(context.Background(), input)
	require.NoError(t, err)
	assert.Empty(t, out.Documents)
}

func TestSuggestionAnalyzer_ImplementsInterface(t *testing.T) {
	var _ SpecializedAnalyzer = NewSuggestionAnalyzer(&mockLLMCompleter{})
}
