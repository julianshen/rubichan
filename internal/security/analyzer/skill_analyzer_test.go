package analyzer

import (
	"context"
	"errors"
	"testing"

	"github.com/julianshen/rubichan/internal/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkillAnalyzerAdapterName(t *testing.T) {
	adapter := NewSkillAnalyzerAdapter("my-skill-analyzer", security.CategoryInjection,
		func(_ context.Context, _ []security.AnalysisChunk) ([]security.Finding, error) {
			return nil, nil
		})
	assert.Equal(t, "my-skill-analyzer", adapter.Name())
}

func TestSkillAnalyzerAdapterCategory(t *testing.T) {
	adapter := NewSkillAnalyzerAdapter("test", security.CategoryCryptography,
		func(_ context.Context, _ []security.AnalysisChunk) ([]security.Finding, error) {
			return nil, nil
		})
	assert.Equal(t, security.CategoryCryptography, adapter.Category())
}

func TestSkillAnalyzerAdapterInterface(t *testing.T) {
	var _ security.LLMAnalyzer = NewSkillAnalyzerAdapter("test", security.CategoryInjection,
		func(_ context.Context, _ []security.AnalysisChunk) ([]security.Finding, error) {
			return nil, nil
		})
}

func TestSkillAnalyzerAdapterCallsFunction(t *testing.T) {
	called := false
	expectedFindings := []security.Finding{
		{
			ID:       "SKILL-001",
			Scanner:  "my-skill",
			Severity: security.SeverityHigh,
			Category: security.CategoryInjection,
			Title:    "Custom skill finding",
		},
	}

	adapter := NewSkillAnalyzerAdapter("my-skill", security.CategoryInjection,
		func(ctx context.Context, chunks []security.AnalysisChunk) ([]security.Finding, error) {
			called = true
			assert.Len(t, chunks, 1)
			assert.Equal(t, "main.go", chunks[0].File)
			return expectedFindings, nil
		})

	chunks := []security.AnalysisChunk{
		{File: "main.go", StartLine: 1, EndLine: 10, Content: "func main() {}"},
	}
	findings, err := adapter.Analyze(context.Background(), chunks)
	require.NoError(t, err)
	assert.True(t, called, "analyze function should have been called")
	assert.Equal(t, expectedFindings, findings)
}

func TestSkillAnalyzerAdapterPropagatesError(t *testing.T) {
	expectedErr := errors.New("skill analysis failed")

	adapter := NewSkillAnalyzerAdapter("failing-skill", security.CategoryInjection,
		func(_ context.Context, _ []security.AnalysisChunk) ([]security.Finding, error) {
			return nil, expectedErr
		})

	chunks := []security.AnalysisChunk{
		{File: "test.go", StartLine: 1, EndLine: 5, Content: "func test() {}"},
	}
	findings, err := adapter.Analyze(context.Background(), chunks)
	assert.ErrorIs(t, err, expectedErr)
	assert.Nil(t, findings)
}
