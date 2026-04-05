package evaluator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Check type constants for precondition validation
const (
	CheckTypeFileExists        = "file_exists"
	CheckTypeDirectoryExists   = "directory_exists"
	CheckTypeCommandRecognized = "command_recognized"
)

// SpeculativeCheck defines a pre-execution check (e.g., file exists before read).
type SpeculativeCheck struct {
	PreconditionType string // CheckTypeFileExists, CheckTypeDirectoryExists, CheckTypeCommandRecognized, etc.
}

// SpeculativeConfig configures precondition checks per tool.
type SpeculativeConfig struct {
	Checks map[string]SpeculativeCheck
}

// SpeculativeChecker runs precondition checks to predict whether a tool call
// is likely to succeed. Checks are advisory; failures don't block execution.
type SpeculativeChecker struct {
	config SpeculativeConfig
}

// NewSpeculativeChecker creates a speculative checker.
func NewSpeculativeChecker(config SpeculativeConfig) *SpeculativeChecker {
	return &SpeculativeChecker{config: config}
}

// Evaluate runs precondition checks and returns whether they're likely to succeed.
func (s *SpeculativeChecker) Evaluate(ctx context.Context, req EvaluationRequest) (EvaluationResult, error) {
	check, found := s.config.Checks[req.ToolName]
	if !found {
		// No check defined for this tool
		return EvaluationResult{SchemaValid: true, ConfidentEnough: true, SpeculativeOK: true}, nil
	}

	switch check.PreconditionType {
	case CheckTypeFileExists:
		return s.checkFileExists(req.Input)
	case CheckTypeDirectoryExists:
		return s.checkDirectoryExists(req.Input)
	case CheckTypeCommandRecognized:
		return s.checkCommandRecognized(req.Input)
	default:
		// Unknown check type; don't block
		return EvaluationResult{SchemaValid: true, ConfidentEnough: true, SpeculativeOK: true}, nil
	}
}

func (s *SpeculativeChecker) checkFileExists(input json.RawMessage) (EvaluationResult, error) {
	var inp struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &inp); err != nil {
		return EvaluationResult{SchemaValid: true, ConfidentEnough: true, SpeculativeOK: true}, nil
	}

	if inp.Path == "" {
		return EvaluationResult{SchemaValid: true, ConfidentEnough: true, SpeculativeOK: true}, nil
	}

	// Don't check file existence (TOCTOU anti-pattern).
	// Let the read operation fail naturally if file is missing.
	// This prevents redundant syscalls and race conditions.
	return EvaluationResult{SchemaValid: true, ConfidentEnough: true, SpeculativeOK: true}, nil
}

func (s *SpeculativeChecker) checkDirectoryExists(input json.RawMessage) (EvaluationResult, error) {
	var inp struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &inp); err != nil {
		return EvaluationResult{SchemaValid: true, ConfidentEnough: true, SpeculativeOK: true}, nil
	}

	if inp.Path == "" {
		return EvaluationResult{SchemaValid: true, ConfidentEnough: true, SpeculativeOK: true}, nil
	}

	// Don't check directory existence (TOCTOU anti-pattern).
	// Let the operation fail naturally if directory is missing.
	return EvaluationResult{SchemaValid: true, ConfidentEnough: true, SpeculativeOK: true}, nil
}

const validCommandChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-./:@"

func (s *SpeculativeChecker) checkCommandRecognized(input json.RawMessage) (EvaluationResult, error) {
	var inp struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &inp); err != nil {
		return EvaluationResult{SchemaValid: true, ConfidentEnough: true, SpeculativeOK: true}, nil
	}

	if inp.Command == "" {
		return EvaluationResult{
			SchemaValid:       true,
			ConfidentEnough:   true,
			SpeculativeOK:     false,
			SpeculativeReason: "empty command",
		}, nil
	}

	// Simple heuristic: check if first word looks syntactically valid
	parts := strings.Fields(inp.Command)
	if len(parts) == 0 {
		return EvaluationResult{
			SchemaValid:       true,
			ConfidentEnough:   true,
			SpeculativeOK:     false,
			SpeculativeReason: "empty command",
		}, nil
	}

	firstWord := parts[0]
	for _, ch := range firstWord {
		if !strings.ContainsRune(validCommandChars, ch) {
			return EvaluationResult{
				SchemaValid:       true,
				ConfidentEnough:   true,
				SpeculativeOK:     false,
				SpeculativeReason: fmt.Sprintf("command looks malformed: %s", firstWord),
			}, nil
		}
	}

	return EvaluationResult{SchemaValid: true, ConfidentEnough: true, SpeculativeOK: true}, nil
}
