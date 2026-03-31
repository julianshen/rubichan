package shell

import (
	"context"
	"fmt"
	"runtime"
	"strings"
)

// ScriptGenerator generates shell scripts from natural language prompts.
type ScriptGenerator struct {
	agentTurn AgentTurnFunc
	shellExec ShellExecFunc
	workDir   *string
}

// NewScriptGenerator creates a script generator.
func NewScriptGenerator(agentTurn AgentTurnFunc, shellExec ShellExecFunc, workDir *string) *ScriptGenerator {
	return &ScriptGenerator{
		agentTurn: agentTurn,
		shellExec: shellExec,
		workDir:   workDir,
	}
}

const scriptGenPrompt = `Generate a shell script that accomplishes the following task:
%s

Working directory: %s
Shell: bash
Platform: %s/%s

Rules:
- Output ONLY the script, no explanation
- Use bash with set -euo pipefail
- Use relative paths from the working directory
- Include brief comments for non-obvious steps
- Prefer standard Unix tools over exotic ones`

// Generate creates a shell script from a natural language prompt.
func (sg *ScriptGenerator) Generate(ctx context.Context, prompt string) (string, error) {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return "", fmt.Errorf("empty prompt")
	}

	if sg.agentTurn == nil {
		return "", fmt.Errorf("agent not available")
	}

	workDir := ""
	if sg.workDir != nil {
		workDir = *sg.workDir
	}

	fullPrompt := fmt.Sprintf(scriptGenPrompt, trimmed, workDir, runtime.GOOS, runtime.GOARCH)

	events, err := sg.agentTurn(ctx, fullPrompt)
	if err != nil {
		return "", fmt.Errorf("generating script: %w", err)
	}

	return extractScript(collectTurnText(events)), nil
}

// Execute runs an approved script and returns its output.
func (sg *ScriptGenerator) Execute(ctx context.Context, script string) (string, string, int, error) {
	if sg.shellExec == nil {
		return "", "", 1, fmt.Errorf("shell execution not available")
	}

	workDir := ""
	if sg.workDir != nil {
		workDir = *sg.workDir
	}

	return sg.shellExec(ctx, script, workDir)
}

// extractScript strips markdown code fences from the response if present.
func extractScript(response string) string {
	trimmed := strings.TrimSpace(response)

	// Try to extract from ```bash or ``` fences
	if idx := strings.Index(trimmed, "```"); idx >= 0 {
		// Find the start of the code block content
		start := idx + 3
		// Skip optional language tag (e.g., "bash", "sh")
		if newline := strings.Index(trimmed[start:], "\n"); newline >= 0 {
			start += newline + 1
		}

		// Find the closing fence
		if end := strings.Index(trimmed[start:], "```"); end >= 0 {
			return strings.TrimSpace(trimmed[start : start+end])
		}
		// No closing fence — use everything after the opening
		return strings.TrimSpace(trimmed[start:])
	}

	return trimmed
}
