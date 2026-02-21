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

// ─── Direct unit tests for base.go helper functions ─────────────────────────

func TestParseFindingsValidJSON(t *testing.T) {
	input := `[{"id":"F-001","title":"Test","severity":"high","category":"injection","description":"desc","location":{"file":"a.go","start_line":1,"end_line":5,"function":"main"},"cwe":"CWE-79","confidence":"medium","remediation":"fix it"}]`
	findings, err := parseFindings(input)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, "F-001", findings[0].ID)
	assert.Equal(t, "high", findings[0].Severity)
	assert.Equal(t, "medium", findings[0].Confidence)
}

func TestParseFindingsEmptyArray(t *testing.T) {
	findings, err := parseFindings("[]")
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestParseFindingsInvalidJSON(t *testing.T) {
	_, err := parseFindings("not json at all")
	assert.Error(t, err)
}

func TestParseFindingsMarkdownCodeFenceJSON(t *testing.T) {
	input := "```json\n" + `[{"id":"F-002","title":"Fenced","severity":"low","category":"test","description":"desc","location":{"file":"b.go","start_line":1,"end_line":2,"function":""},"cwe":"","confidence":"low","remediation":""}]` + "\n```"
	findings, err := parseFindings(input)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, "F-002", findings[0].ID)
	assert.Equal(t, "Fenced", findings[0].Title)
}

func TestParseFindingsMarkdownCodeFenceWithoutClosing(t *testing.T) {
	// Code fence opening without proper closing backticks on last line.
	// The trailing text is included in the JSON parse attempt, causing failure.
	input := "```json\n" + `[{"id":"F-003","title":"NoClose"}]` + "\nsome trailing text"
	_, err := parseFindings(input)
	assert.Error(t, err, "trailing non-JSON text after code fence should fail")
}

func TestParseFindingsMarkdownCodeFenceWithClosing(t *testing.T) {
	// Code fence with proper closing backticks.
	innerJSON := `[{"id":"F-003","title":"WithClose","severity":"info","category":"test","description":"d","location":{"file":"c.go","start_line":1,"end_line":1,"function":""},"cwe":"","confidence":"low","remediation":""}]`
	input := "```\n" + innerJSON + "\n```"
	findings, err := parseFindings(input)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, "F-003", findings[0].ID)
}

func TestParseFindingsMarkdownCodeFenceSingleLine(t *testing.T) {
	// Edge case: code fence with only one line (just the opening ```)
	input := "```json"
	_, err := parseFindings(input)
	assert.Error(t, err, "single-line code fence with no JSON should fail to parse")
}

func TestParseFindingsWhitespace(t *testing.T) {
	input := "   \n  []  \n  "
	findings, err := parseFindings(input)
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestMapSeverityAllValues(t *testing.T) {
	tests := []struct {
		input    string
		expected security.Severity
	}{
		{"critical", security.SeverityCritical},
		{"CRITICAL", security.SeverityCritical},
		{"Critical", security.SeverityCritical},
		{"high", security.SeverityHigh},
		{"HIGH", security.SeverityHigh},
		{"medium", security.SeverityMedium},
		{"MEDIUM", security.SeverityMedium},
		{"low", security.SeverityLow},
		{"LOW", security.SeverityLow},
		{"info", security.SeverityInfo},
		{"INFO", security.SeverityInfo},
		{"unknown", security.SeverityInfo},
		{"", security.SeverityInfo},
		{"something-else", security.SeverityInfo},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, mapSeverity(tc.input))
		})
	}
}

func TestMapConfidenceAllValues(t *testing.T) {
	tests := []struct {
		input    string
		expected security.Confidence
	}{
		{"high", security.ConfidenceHigh},
		{"HIGH", security.ConfidenceHigh},
		{"High", security.ConfidenceHigh},
		{"medium", security.ConfidenceMedium},
		{"MEDIUM", security.ConfidenceMedium},
		{"low", security.ConfidenceLow},
		{"LOW", security.ConfidenceLow},
		{"unknown", security.ConfidenceLow},
		{"", security.ConfidenceLow},
		{"something-else", security.ConfidenceLow},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, mapConfidence(tc.input))
		})
	}
}

func TestBuildUserMessageMultipleChunks(t *testing.T) {
	chunks := []security.AnalysisChunk{
		{File: "a.go", StartLine: 1, EndLine: 10, Content: "func a() {}"},
		{File: "b.go", StartLine: 20, EndLine: 30, Content: "func b() {}"},
	}
	result := buildUserMessage(chunks)
	assert.Contains(t, result, "// File: a.go:1-10")
	assert.Contains(t, result, "func a() {}")
	assert.Contains(t, result, "// File: b.go:20-30")
	assert.Contains(t, result, "func b() {}")
	assert.Contains(t, result, "Analyze the following code segments")
}

func TestBuildUserMessageSingleChunk(t *testing.T) {
	chunks := []security.AnalysisChunk{
		{File: "main.go", StartLine: 5, EndLine: 15, Content: "package main"},
	}
	result := buildUserMessage(chunks)
	assert.Contains(t, result, "// File: main.go:5-15")
	assert.Contains(t, result, "package main")
}

func TestCollectStreamResponseMultipleEvents(t *testing.T) {
	ch := make(chan provider.StreamEvent, 5)
	ch <- provider.StreamEvent{Type: "text_delta", Text: "hello "}
	ch <- provider.StreamEvent{Type: "ping"}
	ch <- provider.StreamEvent{Type: "text_delta", Text: "world"}
	ch <- provider.StreamEvent{Type: "stop"}
	close(ch)

	result := collectStreamResponse(ch)
	assert.Equal(t, "hello world", result)
}

func TestCollectStreamResponseEmptyChannel(t *testing.T) {
	ch := make(chan provider.StreamEvent)
	close(ch)

	result := collectStreamResponse(ch)
	assert.Equal(t, "", result)
}

func TestAnalyzeWithAllSeveritiesAndConfidences(t *testing.T) {
	// Build a response with various severity/confidence combinations.
	findings := []analyzedFinding{
		{ID: "T-001", Title: "Critical finding", Severity: "critical", Confidence: "high"},
		{ID: "T-002", Title: "Medium finding", Severity: "medium", Confidence: "medium"},
		{ID: "T-003", Title: "Low finding", Severity: "low", Confidence: "low"},
		{ID: "T-004", Title: "Info finding", Severity: "info", Confidence: "unknown"},
		{ID: "T-005", Title: "Unknown sev", Severity: "weird", Confidence: "weird"},
	}
	data, err := json.Marshal(findings)
	require.NoError(t, err)

	mock := &mockLLMProvider{response: string(data)}
	a := NewAuthAnalyzer(mock)

	result, err := a.Analyze(context.Background(), sampleChunks())
	require.NoError(t, err)
	require.Len(t, result, 5)

	assert.Equal(t, security.SeverityCritical, result[0].Severity)
	assert.Equal(t, security.ConfidenceHigh, result[0].Confidence)
	assert.Equal(t, security.SeverityMedium, result[1].Severity)
	assert.Equal(t, security.ConfidenceMedium, result[1].Confidence)
	assert.Equal(t, security.SeverityLow, result[2].Severity)
	assert.Equal(t, security.ConfidenceLow, result[2].Confidence)
	assert.Equal(t, security.SeverityInfo, result[3].Severity)
	assert.Equal(t, security.ConfidenceLow, result[3].Confidence) // unknown -> low
	assert.Equal(t, security.SeverityInfo, result[4].Severity)    // weird -> info
	assert.Equal(t, security.ConfidenceLow, result[4].Confidence) // weird -> low
}

func TestAnalyzeWithMarkdownFencedResponse(t *testing.T) {
	innerJSON := `[{"id":"FENCED-001","title":"Fenced finding","severity":"high","category":"test","description":"found","location":{"file":"test.go","start_line":1,"end_line":5,"function":"foo"},"cwe":"CWE-100","confidence":"high","remediation":"fix"}]`
	fencedResponse := "```json\n" + innerJSON + "\n```"

	mock := &mockLLMProvider{response: fencedResponse}
	a := NewAuthAnalyzer(mock)

	result, err := a.Analyze(context.Background(), sampleChunks())
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "FENCED-001", result[0].ID)
	assert.Equal(t, security.SeverityHigh, result[0].Severity)
}

func TestAnalyzeWithEmptyResponse(t *testing.T) {
	mock := &mockLLMProvider{response: ""}
	a := NewAuthAnalyzer(mock)

	result, err := a.Analyze(context.Background(), sampleChunks())
	require.NoError(t, err)
	// Empty string is not valid JSON, so should return unparseable finding.
	require.Len(t, result, 1)
	assert.Equal(t, "Unparseable LLM response", result[0].Title)
}

func TestAnalyzeContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mock := &mockLLMProvider{err: ctx.Err()}
	a := NewAuthAnalyzer(mock)

	_, err := a.Analyze(ctx, sampleChunks())
	assert.Error(t, err)
}
