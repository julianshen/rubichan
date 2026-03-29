package wiki

import (
	"bytes"
	"context"
	"fmt"
	"strings"
)

// SuggestionAnalyzer implements SpecializedAnalyzer to produce improvement suggestions.
type SuggestionAnalyzer struct {
	llm LLMCompleter
}

// NewSuggestionAnalyzer creates a SuggestionAnalyzer backed by the given LLM.
func NewSuggestionAnalyzer(llm LLMCompleter) *SuggestionAnalyzer {
	return &SuggestionAnalyzer{llm: llm}
}

// Name returns the analyzer's identifier.
func (a *SuggestionAnalyzer) Name() string { return "suggestions" }

// Analyze generates improvement suggestions from the architecture description.
// Returns empty output when Architecture is empty or when the LLM call fails (non-fatal).
func (a *SuggestionAnalyzer) Analyze(ctx context.Context, input AnalyzerInput) (*AnalyzerOutput, error) {
	if input.Architecture == "" {
		return &AnalyzerOutput{}, nil
	}

	summaries := buildSummariesText(input.ModuleAnalyses)

	var buf bytes.Buffer
	err := suggestionsTmpl.Execute(&buf, struct {
		Architecture string
		Summaries    string
	}{
		Architecture: input.Architecture,
		Summaries:    summaries,
	})
	if err != nil {
		return &AnalyzerOutput{}, nil // non-fatal
	}

	resp, err := a.llm.Complete(ctx, buf.String())
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return &AnalyzerOutput{}, nil // non-fatal for LLM errors
	}

	resp = strings.TrimSpace(resp)
	if resp == "" {
		return &AnalyzerOutput{}, nil
	}

	doc := Document{
		Path:    "suggestions/improvements.md",
		Title:   "Improvement Suggestions",
		Content: fmt.Sprintf("# Improvement Suggestions\n\n%s\n", resp),
	}

	return &AnalyzerOutput{Documents: []Document{doc}}, nil
}
