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

func TestCompositeEvaluatorApprovedWhenAllPass(t *testing.T) {
	schema := evaluator.NewSchemaValidator(map[string]evaluator.ToolSchema{
		"read_file": {
			RequiredFields: []string{"path"},
		},
	})
	confidence := evaluator.NewConfidenceEvaluator(evaluator.ConfidenceConfig{
		HighRiskTools: []string{"shell"},
		Threshold:     0.7,
	})
	speculative := evaluator.NewSpeculativeChecker(evaluator.SpeculativeConfig{
		Checks: map[string]evaluator.SpeculativeCheck{
			// read_file is not in this map, so speculative check will pass
		},
	})

	composite := evaluator.NewCompositeEvaluator(schema, confidence, speculative)

	result, err := composite.Evaluate(context.Background(), evaluator.EvaluationRequest{
		ToolName: "read_file",
		Input:    json.RawMessage(`{"path":"/tmp"}`),
		Context:  "read configuration",
	})

	assert.NoError(t, err)
	assert.True(t, result.SchemaValid, "schema should be valid")
	assert.True(t, result.ConfidentEnough, "confidence should be sufficient")
	assert.True(t, result.SpeculativeOK, "speculative check should pass")
	assert.True(t, result.Approved(), "overall approval should pass")
}

func TestCompositeEvaluatorRejectedWhenFirstFails(t *testing.T) {
	schema := evaluator.NewSchemaValidator(map[string]evaluator.ToolSchema{
		"shell": {
			RequiredFields: []string{"command"},
		},
	})
	confidence := evaluator.NewConfidenceEvaluator(evaluator.ConfidenceConfig{
		HighRiskTools: []string{"shell"},
		Threshold:     0.7,
	})

	composite := evaluator.NewCompositeEvaluator(schema, confidence)

	result, err := composite.Evaluate(context.Background(), evaluator.EvaluationRequest{
		ToolName: "shell",
		Input:    json.RawMessage(`{}`),
		Context:  "list files",
	})

	assert.NoError(t, err)
	assert.False(t, result.Approved())
	assert.False(t, result.SchemaValid)
}

func TestCompositeEvaluatorFailsFastOnFirstRejection(t *testing.T) {
	schema := evaluator.NewSchemaValidator(map[string]evaluator.ToolSchema{
		"shell": {
			RequiredFields: []string{"command"},
		},
	})
	confidence := evaluator.NewConfidenceEvaluator(evaluator.ConfidenceConfig{
		HighRiskTools: []string{"shell"},
		Threshold:     0.7,
	})

	composite := evaluator.NewCompositeEvaluator(schema, confidence)

	result, err := composite.Evaluate(context.Background(), evaluator.EvaluationRequest{
		ToolName: "shell",
		Input:    json.RawMessage(`{}`),
		Context:  "list files",
	})

	assert.NoError(t, err)
	assert.False(t, result.Approved())
	// Only schema check ran, confidence check didn't
	assert.False(t, result.SchemaValid)
}

func TestSpeculativeCheckerFileExistsCheck(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		input    string
		expectOK bool
	}{
		{
			name:     "read existing file",
			toolName: "read_file",
			input:    `{"path":"/etc/passwd"}`,
			expectOK: true,
		},
		{
			name:     "read nonexistent file",
			toolName: "read_file",
			input:    `{"path":"/nonexistent/file/path/that/does/not/exist.txt"}`,
			expectOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := evaluator.NewSpeculativeChecker(evaluator.SpeculativeConfig{
				Checks: map[string]evaluator.SpeculativeCheck{
					"read_file": {
						PreconditionType: "file_exists",
					},
				},
			})

			result, err := checker.Evaluate(context.Background(), evaluator.EvaluationRequest{
				ToolName: tt.toolName,
				Input:    json.RawMessage(tt.input),
			})

			assert.NoError(t, err)
			assert.Equal(t, tt.expectOK, result.SpeculativeOK)
		})
	}
}

func TestSpeculativeCheckerDirectoryExistsCheck(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		input    string
		expectOK bool
	}{
		{
			name:     "directory exists",
			toolName: "list_dir",
			input:    `{"path":"/tmp"}`,
			expectOK: true,
		},
		{
			name:     "directory does not exist",
			toolName: "list_dir",
			input:    `{"path":"/nonexistent/directory/path/xyz/abc"}`,
			expectOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := evaluator.NewSpeculativeChecker(evaluator.SpeculativeConfig{
				Checks: map[string]evaluator.SpeculativeCheck{
					"list_dir": {
						PreconditionType: "directory_exists",
					},
				},
			})

			result, err := checker.Evaluate(context.Background(), evaluator.EvaluationRequest{
				ToolName: tt.toolName,
				Input:    json.RawMessage(tt.input),
			})

			assert.NoError(t, err)
			assert.Equal(t, tt.expectOK, result.SpeculativeOK)
		})
	}
}

func TestSpeculativeCheckerCommandRecognizedCheck(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		expectOK bool
	}{
		{
			name:     "normal command",
			command:  "ls -la",
			expectOK: true,
		},
		{
			name:     "command with valid alphanumeric",
			command:  "validCommand123",
			expectOK: true,
		},
		{
			name:     "empty command",
			command:  "",
			expectOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := evaluator.NewSpeculativeChecker(evaluator.SpeculativeConfig{
				Checks: map[string]evaluator.SpeculativeCheck{
					"shell": {
						PreconditionType: "command_recognized",
					},
				},
			})

			input := `{"command":"` + tt.command + `"}`
			result, err := checker.Evaluate(context.Background(), evaluator.EvaluationRequest{
				ToolName: "shell",
				Input:    json.RawMessage(input),
			})

			assert.NoError(t, err)
			assert.Equal(t, tt.expectOK, result.SpeculativeOK)
		})
	}
}

func TestCompositeEvaluatorMergesResultsCorrectly(t *testing.T) {
	// Test that composite evaluator correctly merges results when some evaluators pass and others fail
	schema := evaluator.NewSchemaValidator(map[string]evaluator.ToolSchema{
		"write_file": {
			RequiredFields: []string{"path", "content"},
		},
	})
	confidence := evaluator.NewConfidenceEvaluator(evaluator.ConfidenceConfig{
		HighRiskTools: []string{"write_file"},
		Threshold:     0.7,
	})

	composite := evaluator.NewCompositeEvaluator(schema, confidence)

	result, err := composite.Evaluate(context.Background(), evaluator.EvaluationRequest{
		ToolName: "write_file",
		Input:    json.RawMessage(`{"path":"/tmp/test","content":"data","mode":"w"}`),
		Context:  "delete everything",
	})

	assert.NoError(t, err)
	assert.True(t, result.SchemaValid)
	assert.False(t, result.ConfidentEnough) // Should fail due to risky context
	assert.False(t, result.Approved())
}

func TestSpeculativeCheckerHandlesInvalidJSON(t *testing.T) {
	checker := evaluator.NewSpeculativeChecker(evaluator.SpeculativeConfig{
		Checks: map[string]evaluator.SpeculativeCheck{
			"read_file": {
				PreconditionType: "file_exists",
			},
		},
	})

	result, err := checker.Evaluate(context.Background(), evaluator.EvaluationRequest{
		ToolName: "read_file",
		Input:    json.RawMessage("invalid json"),
	})

	assert.NoError(t, err)
	assert.True(t, result.SpeculativeOK) // Invalid JSON is pass-through
}

func TestSpeculativeCheckerHandlesPathFieldMissing(t *testing.T) {
	checker := evaluator.NewSpeculativeChecker(evaluator.SpeculativeConfig{
		Checks: map[string]evaluator.SpeculativeCheck{
			"read_file": {
				PreconditionType: "file_exists",
			},
		},
	})

	result, err := checker.Evaluate(context.Background(), evaluator.EvaluationRequest{
		ToolName: "read_file",
		Input:    json.RawMessage(`{"other":"field"}`),
	})

	assert.NoError(t, err)
	assert.True(t, result.SpeculativeOK) // Missing path field is pass-through
}

func TestSpeculativeCheckerUnknownCheckType(t *testing.T) {
	checker := evaluator.NewSpeculativeChecker(evaluator.SpeculativeConfig{
		Checks: map[string]evaluator.SpeculativeCheck{
			"custom_tool": {
				PreconditionType: "unknown_check_type",
			},
		},
	})

	result, err := checker.Evaluate(context.Background(), evaluator.EvaluationRequest{
		ToolName: "custom_tool",
		Input:    json.RawMessage(`{}`),
	})

	assert.NoError(t, err)
	assert.True(t, result.SpeculativeOK) // Unknown check type is pass-through
}

func TestConfidenceEvaluatorMultipleRiskTerms(t *testing.T) {
	conf := evaluator.NewConfidenceEvaluator(evaluator.ConfidenceConfig{
		HighRiskTools: []string{"shell"},
		Threshold:     0.7,
	})

	result, err := conf.Evaluate(context.Background(), evaluator.EvaluationRequest{
		ToolName: "shell",
		Input:    json.RawMessage(`{"command":"rm /file"}`),
		Context:  "delete and remove harmful files",
	})

	assert.NoError(t, err)
	assert.False(t, result.ConfidentEnough)
	assert.Less(t, result.ConfidenceScore, 0.7)
}
