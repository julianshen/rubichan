package ssecompat

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nopCloser wraps a reader with a no-op Close.
type nopCloser struct{ io.Reader }

func (nopCloser) Close() error { return nil }

func sseBody(lines ...string) io.ReadCloser {
	return nopCloser{strings.NewReader(strings.Join(lines, "\n") + "\n")}
}

func collect(ch <-chan provider.StreamEvent) []provider.StreamEvent {
	var events []provider.StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	return events
}

func TestProcessSSE_TextDelta(t *testing.T) {
	body := sseBody(
		`data: {"id":"chatcmpl-1","model":"gpt-4o","choices":[{"delta":{"content":"Hello"}}]}`,
		`data: {"id":"chatcmpl-1","model":"gpt-4o","choices":[{"delta":{"content":" world"}}]}`,
		`data: [DONE]`,
	)

	ch := make(chan provider.StreamEvent, 16)
	ProcessSSE(context.Background(), body, ch, "test")
	events := collect(ch)

	var texts []string
	for _, ev := range events {
		if ev.Type == "text_delta" {
			texts = append(texts, ev.Text)
		}
	}
	assert.Equal(t, []string{"Hello", " world"}, texts)
}

func TestProcessSSE_MessageStart(t *testing.T) {
	body := sseBody(
		`data: {"id":"chatcmpl-42","model":"gpt-4o","choices":[{"delta":{"content":"hi"}}]}`,
		`data: [DONE]`,
	)

	ch := make(chan provider.StreamEvent, 16)
	ProcessSSE(context.Background(), body, ch, "test")
	events := collect(ch)

	require.GreaterOrEqual(t, len(events), 1)
	assert.Equal(t, "message_start", events[0].Type)
	assert.Equal(t, "gpt-4o", events[0].Model)
	assert.Equal(t, "chatcmpl-42", events[0].MessageID)
}

func TestProcessSSE_ToolCallAccumulation(t *testing.T) {
	body := sseBody(
		`data: {"id":"c1","model":"gpt-4o","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":""}}]}}]}`,
		`data: {"id":"c1","model":"gpt-4o","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":"}}]}}]}`,
		`data: {"id":"c1","model":"gpt-4o","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"/tmp\"}"}}]}}]}`,
		`data: [DONE]`,
	)

	ch := make(chan provider.StreamEvent, 16)
	ProcessSSE(context.Background(), body, ch, "test")
	events := collect(ch)

	var toolUses []provider.StreamEvent
	for _, ev := range events {
		if ev.Type == "tool_use" {
			toolUses = append(toolUses, ev)
		}
	}

	require.Len(t, toolUses, 1)
	assert.Equal(t, "call_1", toolUses[0].ToolUse.ID)
	assert.Equal(t, "read", toolUses[0].ToolUse.Name)
	assert.JSONEq(t, `{"path":"/tmp"}`, string(toolUses[0].ToolUse.Input))
}

func TestProcessSSE_Done(t *testing.T) {
	body := sseBody(
		`data: {"id":"c1","model":"m","choices":[{"delta":{"content":"x"}}]}`,
		`data: [DONE]`,
	)

	ch := make(chan provider.StreamEvent, 16)
	ProcessSSE(context.Background(), body, ch, "test")
	events := collect(ch)

	require.NotEmpty(t, events, "expected at least one event")
	assert.Equal(t, "stop", events[len(events)-1].Type)
}

func TestProcessSSE_ParseError(t *testing.T) {
	body := sseBody(
		`data: {invalid json`,
		`data: [DONE]`,
	)

	ch := make(chan provider.StreamEvent, 16)
	ProcessSSE(context.Background(), body, ch, "test")
	events := collect(ch)

	var hasError bool
	for _, ev := range events {
		if ev.Type == "error" {
			hasError = true
		}
	}
	assert.True(t, hasError, "should emit error event for malformed JSON")
}

func TestProcessSSE_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	body := sseBody(
		`data: {"id":"c1","model":"m","choices":[{"delta":{"content":"x"}}]}`,
		`data: [DONE]`,
	)

	ch := make(chan provider.StreamEvent, 16)
	ProcessSSE(ctx, body, ch, "test")
	events := collect(ch)

	// Should get an error event from context cancellation, not normal processing.
	var hasError bool
	for _, ev := range events {
		if ev.Type == "error" {
			hasError = true
		}
	}
	assert.True(t, hasError)
}

func TestProcessSSE_SkipsNonDataLines(t *testing.T) {
	body := sseBody(
		`: comment`,
		`event: ping`,
		`data: {"id":"c1","model":"m","choices":[{"delta":{"content":"ok"}}]}`,
		``,
		`data: [DONE]`,
	)

	ch := make(chan provider.StreamEvent, 16)
	ProcessSSE(context.Background(), body, ch, "test")
	events := collect(ch)

	var texts []string
	for _, ev := range events {
		if ev.Type == "text_delta" {
			texts = append(texts, ev.Text)
		}
	}
	assert.Equal(t, []string{"ok"}, texts)
}

func TestToolCallAccumulator_MultipleIndexes(t *testing.T) {
	var acc ToolCallAccumulator

	acc.Update(chunkToolCall{Index: 0, ID: "c1", Function: chunkToolFunc{Name: "read", Arguments: `{"a":1}`}})
	acc.Update(chunkToolCall{Index: 1, ID: "c2", Function: chunkToolFunc{Name: "write", Arguments: `{"b":2}`}})

	ch := make(chan provider.StreamEvent, 16)
	ctx := context.Background()
	acc.Flush(ctx, ch)
	close(ch)

	var toolUses []provider.StreamEvent
	for ev := range ch {
		if ev.Type == "tool_use" {
			toolUses = append(toolUses, ev)
		}
	}

	require.Len(t, toolUses, 2)
	assert.Equal(t, "c1", toolUses[0].ToolUse.ID)
	assert.Equal(t, "c2", toolUses[1].ToolUse.ID)
}

func TestProcessSSE_ScannerError(t *testing.T) {
	pr, pw := io.Pipe()
	pw.CloseWithError(errors.New("EOF mid-stream"))

	ch := make(chan provider.StreamEvent, 4)
	ProcessSSE(context.Background(), io.NopCloser(pr), ch, "openai-compat")

	var events []provider.StreamEvent
	for e := range ch {
		events = append(events, e)
	}

	require.Len(t, events, 1)
	errEvt := &events[0]
	require.Equal(t, agentsdk.EventError, errEvt.Type)
	var pe *provider.ProviderError
	require.ErrorAs(t, errEvt.Error, &pe)
	assert.Equal(t, provider.ErrStreamError, pe.Kind)
	assert.Equal(t, "openai-compat", pe.Provider)
	assert.True(t, pe.IsRetryable())
}
