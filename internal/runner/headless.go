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
			Prompt:     prompt,
			Mode:       mode,
			DurationMs: time.Since(start).Milliseconds(),
			Error:      err.Error(),
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
					ID:    evt.ToolCall.ID,
					Name:  evt.ToolCall.Name,
					Input: json.RawMessage(evt.ToolCall.Input),
				})
			}
		case "tool_result":
			if evt.ToolResult != nil {
				for i := range toolCalls {
					if toolCalls[i].ID == evt.ToolResult.ID {
						// Prefer DisplayContent for user-facing output.
						result := evt.ToolResult.DisplayContent
						if result == "" {
							result = evt.ToolResult.Content
						}
						toolCalls[i].Result = result
						toolCalls[i].IsError = evt.ToolResult.IsError
						break
					}
				}
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
		Prompt:     prompt,
		Response:   textBuf.String(),
		ToolCalls:  toolCalls,
		TurnCount:  turns,
		DurationMs: time.Since(start).Milliseconds(),
		Mode:       mode,
		Error:      lastErr,
	}, nil
}
