// Package ssecompat provides a shared SSE stream processor for
// OpenAI-compatible LLM APIs (OpenAI, Z.ai, and similar providers).
package ssecompat

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/julianshen/rubichan/internal/provider"
)

// ChatChunk represents a single SSE chunk from an OpenAI-compatible API.
type ChatChunk struct {
	Choices []ChunkChoice `json:"choices"`
}

// ChunkChoice is a single choice within a chat chunk.
type ChunkChoice struct {
	Delta        ChunkDelta `json:"delta"`
	FinishReason *string    `json:"finish_reason"`
}

// ChunkDelta is the incremental content within a choice.
type ChunkDelta struct {
	Content   *string         `json:"content"`
	ToolCalls []ChunkToolCall `json:"tool_calls"`
}

// ChunkToolCall is an incremental tool call fragment.
type ChunkToolCall struct {
	Index    int           `json:"index"`
	ID       string        `json:"id,omitempty"`
	Type     string        `json:"type,omitempty"`
	Function ChunkToolFunc `json:"function,omitempty"`
}

// ChunkToolFunc is the function portion of a tool call fragment.
type ChunkToolFunc struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ToolCallAccumulator tracks in-flight tool calls by their streamed index.
// OpenAI-compatible APIs interleave argument fragments across multiple tool
// calls in the same response, so we accumulate per-index and flush at stream end.
type ToolCallAccumulator struct {
	calls []struct {
		id   string
		name string
		args strings.Builder
	}
}

// Update processes a streamed tool call chunk.
func (a *ToolCallAccumulator) Update(tc ChunkToolCall) {
	for len(a.calls) <= tc.Index {
		a.calls = append(a.calls, struct {
			id   string
			name string
			args strings.Builder
		}{})
	}
	if tc.ID != "" {
		a.calls[tc.Index].id = tc.ID
	}
	if tc.Function.Name != "" {
		a.calls[tc.Index].name = tc.Function.Name
	}
	if tc.Function.Arguments != "" {
		a.calls[tc.Index].args.WriteString(tc.Function.Arguments)
	}
}

// Flush emits all accumulated tool calls as complete StreamEvents.
func (a *ToolCallAccumulator) Flush(ctx context.Context, ch chan<- provider.StreamEvent) {
	for _, call := range a.calls {
		if call.id == "" {
			continue
		}
		args := call.args.String()
		if args == "" {
			args = "{}"
		}
		select {
		case ch <- provider.StreamEvent{
			Type: "tool_use",
			ToolUse: &provider.ToolUseBlock{
				ID:    call.id,
				Name:  call.name,
				Input: json.RawMessage(args),
			},
		}:
		case <-ctx.Done():
			return
		}
		// Emit a text_delta with the full arguments for the agent loop's
		// toolInputBuf accumulation.
		select {
		case ch <- provider.StreamEvent{Type: "text_delta", Text: args}:
		case <-ctx.Done():
			return
		}
	}
}

// ProcessSSE reads OpenAI-compatible SSE lines from a reader and sends
// StreamEvents to the channel. Handles text deltas, tool call accumulation,
// and [DONE] detection. Closes both the body and the channel when done.
func ProcessSSE(ctx context.Context, body io.ReadCloser, ch chan<- provider.StreamEvent) {
	defer close(ch)
	defer body.Close()

	var toolAcc ToolCallAccumulator

	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		if ctx.Err() != nil {
			select {
			case ch <- provider.StreamEvent{Type: "error", Error: ctx.Err()}:
			default:
			}
			return
		}

		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		if data == "[DONE]" {
			toolAcc.Flush(ctx, ch)
			select {
			case ch <- provider.StreamEvent{Type: "stop"}:
			case <-ctx.Done():
			}
			return
		}

		var chunk ChatChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			select {
			case ch <- provider.StreamEvent{Type: "error", Error: fmt.Errorf("parsing chunk: %w", err)}:
			case <-ctx.Done():
			}
			continue
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta

		if delta.Content != nil && *delta.Content != "" {
			select {
			case ch <- provider.StreamEvent{Type: "text_delta", Text: *delta.Content}:
			case <-ctx.Done():
				return
			}
		}

		for _, tc := range delta.ToolCalls {
			toolAcc.Update(tc)
		}
	}

	if err := scanner.Err(); err != nil {
		select {
		case ch <- provider.StreamEvent{Type: "error", Error: err}:
		case <-ctx.Done():
		}
	}
}
