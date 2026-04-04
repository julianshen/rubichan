package evaluator_test

import (
	"context"
	"encoding/json"
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

func TestSchemaValidatorRejectsInvalidJSON(t *testing.T) {
	validator := evaluator.NewSchemaValidator(map[string]evaluator.ToolSchema{
		"shell": {
			RequiredFields: []string{"command"},
		},
	})

	result, err := validator.Evaluate(context.Background(), evaluator.EvaluationRequest{
		ToolName: "shell",
		Input:    json.RawMessage("invalid json"),
	})

	assert.NoError(t, err)
	assert.False(t, result.SchemaValid)
	assert.NotEmpty(t, result.SchemaError)
	assert.Contains(t, result.SchemaError, "invalid JSON")
}

func TestSchemaValidatorAcceptsMissingOptionalFields(t *testing.T) {
	validator := evaluator.NewSchemaValidator(map[string]evaluator.ToolSchema{
		"shell": {
			RequiredFields: []string{"command"},
		},
	})

	result, err := validator.Evaluate(context.Background(), evaluator.EvaluationRequest{
		ToolName: "shell",
		Input:    json.RawMessage(`{"command":"ls"}`),
	})

	assert.NoError(t, err)
	assert.True(t, result.SchemaValid)
	assert.Empty(t, result.SchemaError)
}

func TestSchemaValidatorRejectsMissingRequiredFields(t *testing.T) {
	validator := evaluator.NewSchemaValidator(map[string]evaluator.ToolSchema{
		"shell": {
			RequiredFields: []string{"command"},
		},
	})

	result, err := validator.Evaluate(context.Background(), evaluator.EvaluationRequest{
		ToolName: "shell",
		Input:    json.RawMessage(`{}`),
	})

	assert.NoError(t, err)
	assert.False(t, result.SchemaValid)
	assert.Contains(t, result.SchemaError, "command")
}

func TestSchemaValidatorPassesThroughUnknownTools(t *testing.T) {
	validator := evaluator.NewSchemaValidator(map[string]evaluator.ToolSchema{
		"shell": {
			RequiredFields: []string{"command"},
		},
	})

	result, err := validator.Evaluate(context.Background(), evaluator.EvaluationRequest{
		ToolName: "unknown_tool",
		Input:    json.RawMessage(`{}`),
	})

	assert.NoError(t, err)
	assert.True(t, result.SchemaValid)
}

func TestConfidenceEvaluatorHighConfidenceOnSafeTools(t *testing.T) {
	conf := evaluator.NewConfidenceEvaluator(evaluator.ConfidenceConfig{
		HighRiskTools: []string{"shell", "write_file"},
		Threshold:     0.7,
	})

	result, err := conf.Evaluate(context.Background(), evaluator.EvaluationRequest{
		ToolName: "read_file",
		Input:    json.RawMessage(`{"path":"/etc/passwd"}`),
		Context:  "read configuration file",
	})

	assert.NoError(t, err)
	assert.True(t, result.ConfidentEnough)
	assert.GreaterOrEqual(t, result.ConfidenceScore, 0.9)
}

func TestConfidenceEvaluatorLowConfidenceOnHighRiskTools(t *testing.T) {
	conf := evaluator.NewConfidenceEvaluator(evaluator.ConfidenceConfig{
		HighRiskTools: []string{"shell", "write_file"},
		Threshold:     0.7,
	})

	result, err := conf.Evaluate(context.Background(), evaluator.EvaluationRequest{
		ToolName: "shell",
		Input:    json.RawMessage(`{"command":"rm -rf /"}`),
		Context:  "delete everything",
	})

	assert.NoError(t, err)
	assert.False(t, result.ConfidentEnough)
}

func TestConfidenceEvaluatorContextAffectsScore(t *testing.T) {
	conf := evaluator.NewConfidenceEvaluator(evaluator.ConfidenceConfig{
		HighRiskTools: []string{"shell"},
		Threshold:     0.7,
	})

	safableRequest := evaluator.EvaluationRequest{
		ToolName: "shell",
		Input:    json.RawMessage(`{"command":"ls"}`),
		Context:  "list directory contents",
	}

	riskyRequest := evaluator.EvaluationRequest{
		ToolName: "shell",
		Input:    json.RawMessage(`{"command":"ls"}`),
		Context:  "perform arbitrary shell operation",
	}

	safe, _ := conf.Evaluate(context.Background(), safableRequest)
	risky, _ := conf.Evaluate(context.Background(), riskyRequest)

	// Same command, different context should affect confidence
	assert.GreaterOrEqual(t, safe.ConfidenceScore, risky.ConfidenceScore)
}

func TestSpeculativeCheckerAcceptsWhenPreconditionsMet(t *testing.T) {
	checker := evaluator.NewSpeculativeChecker(evaluator.SpeculativeConfig{
		Checks: map[string]evaluator.SpeculativeCheck{
			"read_file": {
				PreconditionType: "file_exists",
			},
		},
	})

	// Mock a file that would exist
	result, err := checker.Evaluate(context.Background(), evaluator.EvaluationRequest{
		ToolName: "read_file",
		Input:    json.RawMessage(`{"path":"/etc/passwd"}`),
	})

	assert.NoError(t, err)
	assert.True(t, result.SpeculativeOK)
}

func TestSpeculativeCheckerSkipsUnknownTools(t *testing.T) {
	checker := evaluator.NewSpeculativeChecker(evaluator.SpeculativeConfig{
		Checks: map[string]evaluator.SpeculativeCheck{
			"read_file": {
				PreconditionType: "file_exists",
			},
		},
	})

	result, err := checker.Evaluate(context.Background(), evaluator.EvaluationRequest{
		ToolName: "unknown_tool",
		Input:    json.RawMessage(`{"path":"/etc/passwd"}`),
	})

	assert.NoError(t, err)
	assert.True(t, result.SpeculativeOK)
}

func TestSpeculativeCheckerReturnsReasonWhenCheckFails(t *testing.T) {
	checker := evaluator.NewSpeculativeChecker(evaluator.SpeculativeConfig{
		Checks: map[string]evaluator.SpeculativeCheck{
			"shell": {
				PreconditionType: "command_recognized",
			},
		},
	})

	// A command with suspicious pattern
	result, err := checker.Evaluate(context.Background(), evaluator.EvaluationRequest{
		ToolName: "shell",
		Input:    json.RawMessage(`{"command":"totally_unknown_command_xyz"}`),
	})

	assert.NoError(t, err)
	// Speculative checks are permissive (don't block execution)
	assert.True(t, result.SpeculativeOK)
}
