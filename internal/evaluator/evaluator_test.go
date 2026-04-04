package evaluator_test

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/evaluator"
	"github.com/stretchr/testify/assert"
)

func TestEvaluatorInterfaceExists(t *testing.T) {
	var _ evaluator.Evaluator = (*mockEvaluator)(nil)
}

type mockEvaluator struct{}

func (m *mockEvaluator) Evaluate(ctx context.Context, req evaluator.EvaluationRequest) (evaluator.EvaluationResult, error) {
	return evaluator.EvaluationResult{}, nil
}

func TestEvaluationResultApprovedWhenAllChecksPassed(t *testing.T) {
	result := &evaluator.EvaluationResult{
		SchemaValid:     true,
		ConfidentEnough: true,
		SpeculativeOK:   true,
	}
	assert.True(t, result.Approved())
}

func TestEvaluationResultRejectedWhenAnyCheckFailed(t *testing.T) {
	tests := []struct {
		name     string
		result   *evaluator.EvaluationResult
		approved bool
	}{
		{
			name: "schema invalid",
			result: &evaluator.EvaluationResult{
				SchemaValid:     false,
				ConfidentEnough: true,
				SpeculativeOK:   true,
			},
			approved: false,
		},
		{
			name: "low confidence",
			result: &evaluator.EvaluationResult{
				SchemaValid:     true,
				ConfidentEnough: false,
				SpeculativeOK:   true,
			},
			approved: false,
		},
		{
			name: "speculative check failed",
			result: &evaluator.EvaluationResult{
				SchemaValid:     true,
				ConfidentEnough: true,
				SpeculativeOK:   false,
			},
			approved: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.approved, tt.result.Approved())
		})
	}
}

func TestEvaluationResultReasonIsEmpty_WhenApproved(t *testing.T) {
	result := &evaluator.EvaluationResult{
		SchemaValid:     true,
		ConfidentEnough: true,
		SpeculativeOK:   true,
	}
	result.Approved()
	assert.Empty(t, result.Reason)
}

func TestEvaluationResultReasonDescribesFailure(t *testing.T) {
	result := &evaluator.EvaluationResult{
		SchemaValid:     false,
		ConfidentEnough: true,
		SpeculativeOK:   true,
		SchemaError:     "missing required field: command",
	}
	result.Approved()
	assert.Contains(t, result.Reason, "schema")
}
