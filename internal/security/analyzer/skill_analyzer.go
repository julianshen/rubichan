package analyzer

import (
	"context"

	"github.com/julianshen/rubichan/internal/security"
)

// AnalyzeFunc is a function type that performs LLM-style analysis on code chunks.
type AnalyzeFunc func(ctx context.Context, chunks []security.AnalysisChunk) ([]security.Finding, error)

// SkillAnalyzerAdapter wraps an AnalyzeFunc into a security.LLMAnalyzer,
// allowing Security Rule Skills to be plugged into the analyzer pipeline.
type SkillAnalyzerAdapter struct {
	name     string
	category security.Category
	fn       AnalyzeFunc
}

// NewSkillAnalyzerAdapter creates a SkillAnalyzerAdapter with the given name,
// category, and analysis function.
func NewSkillAnalyzerAdapter(name string, cat security.Category, fn AnalyzeFunc) *SkillAnalyzerAdapter {
	return &SkillAnalyzerAdapter{
		name:     name,
		category: cat,
		fn:       fn,
	}
}

// Name returns the analyzer name.
func (a *SkillAnalyzerAdapter) Name() string {
	return a.name
}

// Category returns the security category this analyzer focuses on.
func (a *SkillAnalyzerAdapter) Category() security.Category {
	return a.category
}

// Analyze delegates to the wrapped AnalyzeFunc.
func (a *SkillAnalyzerAdapter) Analyze(ctx context.Context, chunks []security.AnalysisChunk) ([]security.Finding, error) {
	return a.fn(ctx, chunks)
}
