package shell

import (
	"context"
	"fmt"
	"strings"
)

// ErrorAnalyzer provides AI-powered analysis of failed shell commands.
type ErrorAnalyzer struct {
	agentTurn AgentTurnFunc
	enabled   bool
	maxOutput int // max bytes of output to send to LLM
}

// NewErrorAnalyzer creates an error analyzer.
func NewErrorAnalyzer(agentTurn AgentTurnFunc, enabled bool, maxOutput int) *ErrorAnalyzer {
	if maxOutput <= 0 {
		maxOutput = 4096
	}
	return &ErrorAnalyzer{
		agentTurn: agentTurn,
		enabled:   enabled,
		maxOutput: maxOutput,
	}
}

// Analyze sends a failed command's output to the LLM for diagnosis.
// Returns a channel of TurnEvents for streaming the suggestion.
func (ea *ErrorAnalyzer) Analyze(ctx context.Context, command string, stdout string, stderr string, exitCode int) (<-chan TurnEvent, error) {
	if !ea.enabled || ea.agentTurn == nil {
		return nil, nil
	}

	// Build combined output, truncating if needed.
	combined := stdout
	if stderr != "" {
		if combined != "" {
			combined += "\n"
		}
		combined += stderr
	}
	if len(combined) > ea.maxOutput {
		remaining := len(combined) - ea.maxOutput
		combined = combined[:ea.maxOutput] + fmt.Sprintf("\n... (truncated, %d more bytes)", remaining)
	}

	// Build prompt.
	var b strings.Builder
	fmt.Fprintf(&b, "The command `%s` failed with exit code %d.", command, exitCode)
	if combined != "" {
		fmt.Fprintf(&b, " Output:\n```\n%s\n```", combined)
	}
	b.WriteString("\n\nAnalyze the error concisely and suggest a fix. Be brief.")

	return ea.agentTurn(ctx, b.String())
}
