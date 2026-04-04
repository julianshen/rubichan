package evaluator

import (
	"context"
	"strings"
)

// ConfidenceConfig configures confidence thresholds.
type ConfidenceConfig struct {
	HighRiskTools []string
	Threshold     float64 // 0.0-1.0; if score < threshold, approval required
}

// ConfidenceEvaluator assigns confidence scores based on tool riskiness
// and contextual clues. High-risk tools (shell, write) require explicit approval.
type ConfidenceEvaluator struct {
	config ConfidenceConfig
}

// NewConfidenceEvaluator creates a confidence evaluator.
func NewConfidenceEvaluator(config ConfidenceConfig) *ConfidenceEvaluator {
	return &ConfidenceEvaluator{config: config}
}

// Evaluate scores the tool call based on tool risk and context alignment.
func (c *ConfidenceEvaluator) Evaluate(ctx context.Context, req EvaluationRequest) (EvaluationResult, error) {
	isHighRisk := false
	for _, risky := range c.config.HighRiskTools {
		if req.ToolName == risky {
			isHighRisk = true
			break
		}
	}

	if !isHighRisk {
		// Safe tools get high confidence by default
		return EvaluationResult{
			ConfidentEnough: true,
			ConfidenceScore: 0.95,
		}, nil
	}

	// High-risk tools score based on context alignment.
	// Destructive terms in context reduce confidence.
	score := 0.6 // Base score for high-risk tools
	riskTerms := []string{"delete", "remove", "destroy", "erase", "truncate", "rm -rf"}
	lowerContext := strings.ToLower(req.Context)
	for _, term := range riskTerms {
		if strings.Contains(lowerContext, term) {
			score -= 0.2
		}
	}
	if score < 0 {
		score = 0
	}

	return EvaluationResult{
		ConfidentEnough: score >= c.config.Threshold,
		ConfidenceScore: score,
	}, nil
}
