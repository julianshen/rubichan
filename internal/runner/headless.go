// internal/runner/headless.go
package runner

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/output"
)

// TurnFunc matches the signature of agent.Agent.Turn.
type TurnFunc func(ctx context.Context, msg string) (<-chan agent.TurnEvent, error)

// HeadlessRunner executes a single agent turn and collects the result.
type HeadlessRunner struct {
	turn TurnFunc
}

// NewHeadlessRunner creates a new HeadlessRunner with the given turn function.
func NewHeadlessRunner(turn TurnFunc) *HeadlessRunner {
	return &HeadlessRunner{turn: turn}
}

// Run executes the agent with the given prompt and collects a RunResult.
func (r *HeadlessRunner) Run(ctx context.Context, prompt, mode string) (*output.RunResult, error) {
	start := time.Now()

	ch, err := r.turn(ctx, prompt)
	if err != nil {
		return &output.RunResult{
			Prompt:   prompt,
			Mode:     mode,
			Duration: time.Since(start),
			Error:    err.Error(),
		}, nil
	}

	var textBuf strings.Builder
	var toolCalls []output.ToolCallLog
	var lastErr string
	turns := 0

	for evt := range ch {
		switch evt.Type {
		case "text_delta":
			textBuf.WriteString(evt.Text)
		case "tool_call":
			if evt.ToolCall != nil {
				toolCalls = append(toolCalls, output.ToolCallLog{
					Name:  evt.ToolCall.Name,
					Input: json.RawMessage(evt.ToolCall.Input),
				})
			}
		case "tool_result":
			if evt.ToolResult != nil && len(toolCalls) > 0 {
				last := &toolCalls[len(toolCalls)-1]
				last.Result = evt.ToolResult.Content
				last.IsError = evt.ToolResult.IsError
			}
		case "error":
			if evt.Error != nil {
				lastErr = evt.Error.Error()
			}
		case "done":
			turns++
		}
	}

	return &output.RunResult{
		Prompt:    prompt,
		Response:  textBuf.String(),
		ToolCalls: toolCalls,
		TurnCount: turns,
		Duration:  time.Since(start),
		Mode:      mode,
		Error:     lastErr,
	}, nil
}
