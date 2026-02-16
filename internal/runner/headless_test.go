// internal/runner/headless_test.go
package runner

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/agent"
)

// makeEventCh creates a closed channel pre-filled with the given events.
func makeEventCh(events ...agent.TurnEvent) <-chan agent.TurnEvent {
	ch := make(chan agent.TurnEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return ch
}

func TestHeadlessRunnerBasic(t *testing.T) {
	turnFn := func(_ context.Context, msg string) (<-chan agent.TurnEvent, error) {
		return makeEventCh(
			agent.TurnEvent{Type: "text_delta", Text: "Hello "},
			agent.TurnEvent{Type: "text_delta", Text: "World"},
			agent.TurnEvent{Type: "done"},
		), nil
	}

	r := NewHeadlessRunner(turnFn)
	result, err := r.Run(context.Background(), "say hello", "generic")
	require.NoError(t, err)

	assert.Equal(t, "say hello", result.Prompt)
	assert.Equal(t, "Hello World", result.Response)
	assert.Equal(t, "generic", result.Mode)
	assert.Empty(t, result.ToolCalls)
	assert.Empty(t, result.Error)
}

func TestHeadlessRunnerWithToolCalls(t *testing.T) {
	turnFn := func(_ context.Context, msg string) (<-chan agent.TurnEvent, error) {
		return makeEventCh(
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t1", Name: "file", Input: []byte(`{"op":"read"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t1", Name: "file", Content: "package main", IsError: false,
			}},
			agent.TurnEvent{Type: "text_delta", Text: "Done"},
			agent.TurnEvent{Type: "done"},
		), nil
	}

	r := NewHeadlessRunner(turnFn)
	result, err := r.Run(context.Background(), "read file", "generic")
	require.NoError(t, err)

	assert.Equal(t, "Done", result.Response)
	require.Len(t, result.ToolCalls, 1)
	assert.Equal(t, "file", result.ToolCalls[0].Name)
	assert.Equal(t, "package main", result.ToolCalls[0].Result)
}

func TestHeadlessRunnerError(t *testing.T) {
	turnFn := func(_ context.Context, msg string) (<-chan agent.TurnEvent, error) {
		return makeEventCh(
			agent.TurnEvent{Type: "error", Error: assert.AnError},
			agent.TurnEvent{Type: "done"},
		), nil
	}

	r := NewHeadlessRunner(turnFn)
	result, err := r.Run(context.Background(), "fail", "generic")
	require.NoError(t, err)

	assert.NotEmpty(t, result.Error)
}

func TestHeadlessRunnerTimeout(t *testing.T) {
	turnFn := func(ctx context.Context, msg string) (<-chan agent.TurnEvent, error) {
		ch := make(chan agent.TurnEvent)
		go func() {
			defer close(ch)
			<-ctx.Done()
			ch <- agent.TurnEvent{Type: "error", Error: ctx.Err()}
			ch <- agent.TurnEvent{Type: "done"}
		}()
		return ch, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	r := NewHeadlessRunner(turnFn)
	result, err := r.Run(ctx, "slow", "generic")
	require.NoError(t, err)

	assert.NotEmpty(t, result.Error)
}
