package shell

import (
	"context"
	"fmt"
	"strings"
)

// ErrorAnalyzer provides AI-powered analysis of failed shell commands.
type ErrorAnalyzer struct {
	agentTurn AgentTurnFunc
	maxOutput int
}

// NewErrorAnalyzer creates an error analyzer.
func NewErrorAnalyzer(agentTurn AgentTurnFunc, maxOutput int) *ErrorAnalyzer {
	if maxOutput <= 0 {
		maxOutput = 4096
	}
	return &ErrorAnalyzer{
		agentTurn: agentTurn,
		maxOutput: maxOutput,
	}
}

// Analyze sends a failed command's output to the LLM for diagnosis.
// Returns a channel of TurnEvents for streaming the suggestion.
func (ea *ErrorAnalyzer) Analyze(ctx context.Context, command string, stdout string, stderr string, exitCode int) (<-chan TurnEvent, error) {
	if ea.agentTurn == nil {
		return nil, nil
	}

	combined := stdout
	if stderr != "" {
		if combined != "" {
			combined += "\n"
		}
		combined += stderr
	}
	combined = truncateWithNotice(combined, ea.maxOutput)

	var b strings.Builder
	fmt.Fprintf(&b, "The command `%s` failed with exit code %d.", command, exitCode)
	if combined != "" {
		fmt.Fprintf(&b, " Output:\n```\n%s\n```", combined)
	}
	b.WriteString("\n\nAnalyze the error concisely and suggest a fix. Be brief.")

	return ea.agentTurn(ctx, b.String())
}
