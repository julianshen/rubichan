package evaluator

import (
	"context"
	"encoding/json"
	"fmt"
)

// Evaluator checks whether a tool call is valid and safe before execution.
// It implements three checks: schema validation, confidence scoring, and
// speculative checks to prevent obviously-bad tool invocations.
type Evaluator interface {
	Evaluate(ctx context.Context, req EvaluationRequest) (EvaluationResult, error)
}

// EvaluationRequest describes a tool call to evaluate.
type EvaluationRequest struct {
	ToolName string          // Name of the tool (e.g., "shell", "read_file")
	Input    json.RawMessage // Tool input as JSON
	Context  string          // Optional context (e.g., the user's request)
}

// EvaluationResult reports the outcome of all three checks.
type EvaluationResult struct {
	// SchemaValid reports whether input matches tool's JSON schema.
	SchemaValid bool
	SchemaError string

	// ConfidentEnough reports whether model confidence is high enough to execute.
	// Low confidence on risky tools (shell, write) requires human approval.
	ConfidentEnough bool
	ConfidenceScore float64

	// SpeculativeOK reports whether a speculative check suggests success.
	// Example: checking if a file exists before attempting to read it.
	SpeculativeOK     bool
	SpeculativeReason string

	// Reason is a human-readable explanation if Approved() is false.
	Reason string
}

// Approved returns true if all checks passed and execution is safe.
func (r *EvaluationResult) Approved() bool {
	approved := r.SchemaValid && r.ConfidentEnough && r.SpeculativeOK
	if !approved && r.Reason == "" {
		// Build reason from failed checks
		var reasons []string
		if !r.SchemaValid {
			reasons = append(reasons, "schema validation failed: "+r.SchemaError)
		}
		if !r.ConfidentEnough {
			reasons = append(reasons, fmt.Sprintf("confidence too low: %.2f", r.ConfidenceScore))
		}
		if !r.SpeculativeOK {
			reasons = append(reasons, "speculative check failed: "+r.SpeculativeReason)
		}
		if len(reasons) > 0 {
			r.Reason = reasons[0]
		}
	}
	return approved
}

// CompositeEvaluator runs multiple evaluators and combines their results.
// It fails fast: if any evaluator rejects the call, it returns that rejection.
type CompositeEvaluator struct {
	evaluators []Evaluator
}

// NewCompositeEvaluator creates an evaluator that runs multiple checks in sequence.
func NewCompositeEvaluator(evals ...Evaluator) *CompositeEvaluator {
	return &CompositeEvaluator{evaluators: evals}
}

// Evaluate runs each evaluator in sequence, failing fast on first rejection.
func (c *CompositeEvaluator) Evaluate(ctx context.Context, req EvaluationRequest) (EvaluationResult, error) {
	for _, e := range c.evaluators {
		result, err := e.Evaluate(ctx, req)
		if err != nil {
			return EvaluationResult{}, fmt.Errorf("evaluator failed: %w", err)
		}
		if !result.Approved() {
			return result, nil
		}
	}
	return EvaluationResult{
		SchemaValid:     true,
		ConfidentEnough: true,
		SpeculativeOK:   true,
	}, nil
}
