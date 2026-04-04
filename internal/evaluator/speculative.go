package evaluator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// SpeculativeCheck defines a pre-execution check (e.g., file exists before read).
type SpeculativeCheck struct {
	PreconditionType string // "file_exists", "directory_exists", "command_recognized", etc.
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
		return EvaluationResult{SpeculativeOK: true}, nil
	}

	switch check.PreconditionType {
	case "file_exists":
		return s.checkFileExists(req.Input)
	case "directory_exists":
		return s.checkDirectoryExists(req.Input)
	case "command_recognized":
		return s.checkCommandRecognized(req.Input)
	default:
		// Unknown check type; don't block
		return EvaluationResult{SpeculativeOK: true}, nil
	}
}

func (s *SpeculativeChecker) checkFileExists(input json.RawMessage) (EvaluationResult, error) {
	var inp map[string]interface{}
	if err := json.Unmarshal(input, &inp); err != nil {
		return EvaluationResult{SpeculativeOK: true}, nil
	}

	path, ok := inp["path"].(string)
	if !ok {
		return EvaluationResult{SpeculativeOK: true}, nil
	}

	if _, err := os.Stat(path); err == nil {
		return EvaluationResult{SpeculativeOK: true}, nil
	}

	return EvaluationResult{
		SpeculativeOK:     false,
		SpeculativeReason: fmt.Sprintf("file does not exist: %s", path),
	}, nil
}

func (s *SpeculativeChecker) checkDirectoryExists(input json.RawMessage) (EvaluationResult, error) {
	var inp map[string]interface{}
	if err := json.Unmarshal(input, &inp); err != nil {
		return EvaluationResult{SpeculativeOK: true}, nil
	}

	path, ok := inp["path"].(string)
	if !ok {
		return EvaluationResult{SpeculativeOK: true}, nil
	}

	stat, err := os.Stat(path)
	if err != nil || !stat.IsDir() {
		return EvaluationResult{
			SpeculativeOK:     false,
			SpeculativeReason: fmt.Sprintf("directory does not exist: %s", path),
		}, nil
	}

	return EvaluationResult{SpeculativeOK: true}, nil
}

func (s *SpeculativeChecker) checkCommandRecognized(input json.RawMessage) (EvaluationResult, error) {
	var inp map[string]interface{}
	if err := json.Unmarshal(input, &inp); err != nil {
		return EvaluationResult{SpeculativeOK: true}, nil
	}

	cmd, ok := inp["command"].(string)
	if !ok {
		return EvaluationResult{SpeculativeOK: true}, nil
	}

	// Simple heuristic: check if command looks like gibberish
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return EvaluationResult{
			SpeculativeOK:     false,
			SpeculativeReason: "empty command",
		}, nil
	}

	// If the first word contains non-alphanumeric characters (except common ones), flag it
	firstWord := parts[0]
	validChars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-./:"
	for _, ch := range firstWord {
		if !strings.ContainsRune(validChars, ch) {
			return EvaluationResult{
				SpeculativeOK:     false,
				SpeculativeReason: fmt.Sprintf("command looks malformed: %s", firstWord),
			}, nil
		}
	}

	return EvaluationResult{SpeculativeOK: true}, nil
}
