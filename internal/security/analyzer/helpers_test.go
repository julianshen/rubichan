package analyzer

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLLMProvider is a test double for provider.LLMProvider that returns
// a predetermined response or error.
type mockLLMProvider struct {
	response string
	err      error
}

func (m *mockLLMProvider) Stream(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	if m.err != nil {
		return nil, m.err
	}
	ch := make(chan provider.StreamEvent, 2)
	ch <- provider.StreamEvent{Type: "text_delta", Text: m.response}
	ch <- provider.StreamEvent{Type: "stop"}
	close(ch)
	return ch, nil
}

// sampleFindingsJSON returns a valid JSON response containing one finding.
func sampleFindingsJSON(t *testing.T) string {
	t.Helper()
	findings := []analyzedFinding{
		{
			ID:          "TEST-001",
			Title:       "Test vulnerability found",
			Severity:    "high",
			Category:    "test",
			Description: "A test vulnerability was detected in the code.",
			CWE:         "CWE-999",
			Confidence:  "high",
			Remediation: "Fix the test issue.",
		},
	}
	findings[0].Location.File = "main.go"
	findings[0].Location.StartLine = 10
	findings[0].Location.EndLine = 20
	findings[0].Location.Function = "handleRequest"

	data, err := json.Marshal(findings)
	require.NoError(t, err)
	return string(data)
}

// sampleChunks returns a slice with one test AnalysisChunk.
func sampleChunks() []security.AnalysisChunk {
	return []security.AnalysisChunk{
		{
			File:      "main.go",
			StartLine: 1,
			EndLine:   20,
			Content:   "func handleRequest() {}",
			Language:  "go",
			RiskScore: 10,
		},
	}
}

// analyzerTestSuite runs the standard set of tests for any LLM analyzer.
// This eliminates test duplication across the 5 analyzer implementations.
func analyzerTestSuite(t *testing.T, name string, category security.Category, newFn func(provider.LLMProvider) *baseAnalyzer) {
	t.Helper()

	t.Run("Name", func(t *testing.T) {
		a := newFn(&mockLLMProvider{})
		assert.Equal(t, name, a.Name())
	})

	t.Run("Category", func(t *testing.T) {
		a := newFn(&mockLLMProvider{})
		assert.Equal(t, category, a.Category())
	})

	t.Run("Interface", func(t *testing.T) {
		var _ security.LLMAnalyzer = newFn(&mockLLMProvider{})
	})

	t.Run("DetectsFindings", func(t *testing.T) {
		mock := &mockLLMProvider{response: sampleFindingsJSON(t)}
		a := newFn(mock)

		findings, err := a.Analyze(context.Background(), sampleChunks())
		require.NoError(t, err)
		require.Len(t, findings, 1)
		assert.Equal(t, "TEST-001", findings[0].ID)
		assert.Equal(t, name, findings[0].Scanner)
		assert.Equal(t, security.SeverityHigh, findings[0].Severity)
		assert.Equal(t, category, findings[0].Category)
		assert.Equal(t, "main.go", findings[0].Location.File)
		assert.Equal(t, 10, findings[0].Location.StartLine)
		assert.Equal(t, 20, findings[0].Location.EndLine)
		assert.Equal(t, "handleRequest", findings[0].Location.Function)
		assert.Equal(t, security.ConfidenceHigh, findings[0].Confidence)
	})

	t.Run("HandlesMalformedJSON", func(t *testing.T) {
		mock := &mockLLMProvider{response: "This is not valid JSON, just a plain text analysis."}
		a := newFn(mock)

		findings, err := a.Analyze(context.Background(), sampleChunks())
		require.NoError(t, err)
		require.Len(t, findings, 1)
		assert.Equal(t, "Unparseable LLM response", findings[0].Title)
		assert.Equal(t, security.ConfidenceLow, findings[0].Confidence)
		assert.Contains(t, findings[0].Evidence, "This is not valid JSON")
		assert.Equal(t, name, findings[0].Scanner)
	})

	t.Run("EmptyChunks", func(t *testing.T) {
		a := newFn(&mockLLMProvider{})

		findings, err := a.Analyze(context.Background(), nil)
		require.NoError(t, err)
		assert.Nil(t, findings)
	})

	t.Run("ProviderError", func(t *testing.T) {
		mock := &mockLLMProvider{err: assert.AnError}
		a := newFn(mock)

		findings, err := a.Analyze(context.Background(), sampleChunks())
		assert.Error(t, err)
		assert.Nil(t, findings)
		assert.Contains(t, err.Error(), name)
	})
}
