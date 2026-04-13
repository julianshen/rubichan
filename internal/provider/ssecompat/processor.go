// Package ssecompat provides a shared SSE stream processor for
// OpenAI-compatible LLM APIs (OpenAI, Z.ai, and similar providers).
package ssecompat

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// chatChunk represents a single SSE chunk from an OpenAI-compatible API.
type chatChunk struct {
	ID      string        `json:"id"`
	Model   string        `json:"model"`
	Choices []chunkChoice `json:"choices"`
}

type chunkChoice struct {
	Delta        chunkDelta `json:"delta"`
	FinishReason *string    `json:"finish_reason"`
}

type chunkDelta struct {
	Content   *string         `json:"content"`
	ToolCalls []chunkToolCall `json:"tool_calls"`
}

type chunkToolCall struct {
	Index    int           `json:"index"`
	ID       string        `json:"id,omitempty"`
	Type     string        `json:"type,omitempty"`
	Function chunkToolFunc `json:"function,omitempty"`
}

type chunkToolFunc struct {
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
// maxToolCallIndex guards against unbounded slice growth from malformed chunks.
const maxToolCallIndex = 128

func (a *ToolCallAccumulator) Update(tc chunkToolCall) {
	if tc.Index < 0 || tc.Index > maxToolCallIndex {
		return
	}
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
		// Emit text_delta with the full arguments so the agent loop's
		// toolInputBuf captures them (needed for OpenAI-compat providers
		// that don't emit input_json_delta incrementally).
		select {
		case ch <- provider.StreamEvent{Type: "text_delta", Text: args}:
		case <-ctx.Done():
			return
		}
	}
}

// ProcessSSE reads OpenAI-compatible SSE lines from a reader and sends
// StreamEvents to the channel. Handles text deltas, tool call accumulation,
// and [DONE] detection. Closes the channel when done; the watchdog pump
// goroutine owns closing body.
// providerName is included in any ProviderError emitted so callers can
// distinguish which provider encountered the stream failure.
func ProcessSSE(ctx context.Context, body io.ReadCloser, ch chan<- provider.StreamEvent, providerName string) {
	defer close(ch)

	watched := provider.WatchBody(body, provider.WatchdogConfig{}, nil, nil)
	defer watched.Close()

	var toolAcc ToolCallAccumulator
	sentMessageStart := false

	scanner := bufio.NewScanner(watched)
	// Increase buffer to 1MB to handle large JSON chunks (reasoning, tool args).
	const maxScanCapacity = 1024 * 1024
	scanner.Buffer(make([]byte, 0, maxScanCapacity), maxScanCapacity)
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

		var chunk chatChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			select {
			case ch <- provider.StreamEvent{Type: "error", Error: fmt.Errorf("parsing chunk: %w", err)}:
			case <-ctx.Done():
			}
			continue
		}

		// Emit synthetic message_start from the first chunk with model info.
		if !sentMessageStart && chunk.Model != "" {
			sentMessageStart = true
			select {
			case ch <- provider.StreamEvent{
				Type:      "message_start",
				Model:     chunk.Model,
				MessageID: chunk.ID,
			}:
			case <-ctx.Done():
				return
			}
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
		var streamErr *provider.ProviderError
		if !errors.As(err, &streamErr) {
			streamErr = &provider.ProviderError{
				Kind:      provider.ErrStreamError,
				Provider:  providerName,
				Message:   err.Error(),
				Retryable: true,
			}
		} else {
			if streamErr.Provider == "" {
				streamErr.Provider = providerName
			}
		}
		select {
		case ch <- provider.StreamEvent{Type: agentsdk.EventError, Error: streamErr}:
		case <-ctx.Done():
		}
	}
}
