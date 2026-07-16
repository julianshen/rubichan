package agentsdk

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingTool struct{ dummyTool }

func (f *failingTool) Execute(context.Context, json.RawMessage) (ToolResult, error) {
	return ToolResult{}, errors.New("permission denied")
}

type streamingDummy struct {
	dummyTool
	streamed bool
}

func (s *streamingDummy) ExecuteStream(_ context.Context, _ json.RawMessage, emit ToolEventEmitter) (ToolResult, error) {
	s.streamed = true
	emit(ToolEvent{Stage: EventDelta, Content: "chunk"})
	return ToolResult{Content: "streamed done"}, nil
}

// lookupOnly wraps a Registry but hides Names(), so suggestion is disabled.
type lookupOnly struct{ r *Registry }

func (l lookupOnly) Get(name string) (Tool, bool) { return l.r.Get(name) }

func TestExecuteToolSuccess(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&dummyTool{name: "echo"}))

	out := ExecuteTool(context.Background(), r, "echo", json.RawMessage(`{}`), nil)
	assert.False(t, out.IsError)
	assert.Equal(t, "ok", out.Content)
}

func TestExecuteToolUnknownWithSuggestion(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&dummyTool{name: "shell"}))
	require.NoError(t, r.Register(&dummyTool{name: "file"}))

	out := ExecuteTool(context.Background(), r, "shell_exec", nil, nil)
	assert.True(t, out.IsError)
	assert.Contains(t, out.Content, "unknown tool: shell_exec")
	assert.Contains(t, out.Content, `Did you mean "shell"?`)
	// Available tools are listed sorted.
	assert.Contains(t, out.Content, "Available tools: file, shell")
}

func TestExecuteToolUnknownNoNamerNoSuggestion(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&dummyTool{name: "shell"}))

	out := ExecuteTool(context.Background(), lookupOnly{r}, "foobar", nil, nil)
	assert.True(t, out.IsError)
	assert.Equal(t, "unknown tool: foobar", out.Content)
}

func TestExecuteToolUnknownNoMatchKeepsPlainMessage(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&dummyTool{name: "shell"}))

	out := ExecuteTool(context.Background(), r, "zzz", nil, nil)
	assert.True(t, out.IsError)
	assert.Equal(t, "unknown tool: zzz", out.Content)
}

func TestExecuteToolErrorWrapped(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&failingTool{dummyTool{name: "denied"}}))

	out := ExecuteTool(context.Background(), r, "denied", nil, nil)
	assert.True(t, out.IsError)
	assert.Equal(t, "tool execution error: permission denied", out.Content)
}

func TestExecuteToolStreamingWithEmitter(t *testing.T) {
	r := NewRegistry()
	st := &streamingDummy{dummyTool: dummyTool{name: "stream"}}
	require.NoError(t, r.Register(st))

	var events []ToolEvent
	emit := func(ev ToolEvent) { events = append(events, ev) }

	out := ExecuteTool(context.Background(), r, "stream", nil, emit)
	assert.True(t, st.streamed, "streaming path must be used when an emitter is provided")
	assert.Equal(t, "streamed done", out.Content)
	require.Len(t, events, 1)
	assert.Equal(t, "chunk", events[0].Content)
}

func TestExecuteToolStreamingWithoutEmitterFallsBack(t *testing.T) {
	r := NewRegistry()
	st := &streamingDummy{dummyTool: dummyTool{name: "stream"}}
	require.NoError(t, r.Register(st))

	out := ExecuteTool(context.Background(), r, "stream", nil, nil)
	assert.False(t, st.streamed, "no emitter: plain Execute must be used")
	assert.Equal(t, "ok", out.Content)
}

func TestMakeToolCallEvent(t *testing.T) {
	ev := MakeToolCallEvent(ToolUseBlock{ID: "t1", Name: "shell", Input: json.RawMessage(`{"a":1}`)})
	assert.Equal(t, "tool_call", ev.Type)
	require.NotNil(t, ev.ToolCall)
	assert.Equal(t, "t1", ev.ToolCall.ID)
	assert.Equal(t, "shell", ev.ToolCall.Name)
	assert.JSONEq(t, `{"a":1}`, string(ev.ToolCall.Input))
}

func TestMakeToolResultEvent(t *testing.T) {
	ev := MakeToolResultEvent("t1", "shell", "out", "display", true)
	assert.Equal(t, "tool_result", ev.Type)
	require.NotNil(t, ev.ToolResult)
	assert.Equal(t, "t1", ev.ToolResult.ID)
	assert.Equal(t, "shell", ev.ToolResult.Name)
	assert.Equal(t, "out", ev.ToolResult.Content)
	assert.Equal(t, "display", ev.ToolResult.DisplayContent)
	assert.True(t, ev.ToolResult.IsError)
}

func TestMakeToolProgressEmitter(t *testing.T) {
	var got []TurnEvent
	emit := MakeToolProgressEmitter("t1", "shell", func(ev TurnEvent) { got = append(got, ev) })

	emit(ToolEvent{Stage: EventDelta, Content: "line", IsError: true})
	require.Len(t, got, 1)
	assert.Equal(t, "tool_progress", got[0].Type)
	require.NotNil(t, got[0].ToolProgress)
	assert.Equal(t, "t1", got[0].ToolProgress.ID)
	assert.Equal(t, "shell", got[0].ToolProgress.Name)
	assert.Equal(t, EventDelta, got[0].ToolProgress.Stage)
	assert.Equal(t, "line", got[0].ToolProgress.Content)
	assert.True(t, got[0].ToolProgress.IsError)
}

func TestSendEventReturnsOnContextCancelWhenChannelFull(t *testing.T) {
	ch := make(chan TurnEvent) // unbuffered, no reader
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		sendEvent(ctx, ch, TurnEvent{Type: "tool_progress"})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("sendEvent blocked despite cancelled context — goroutine/turnMu leak")
	}
}

func TestSendEventDeliversWhenReaderPresent(t *testing.T) {
	ch := make(chan TurnEvent, 1)
	sendEvent(context.Background(), ch, TurnEvent{Type: "tool_progress"})
	select {
	case ev := <-ch:
		assert.Equal(t, "tool_progress", ev.Type)
	default:
		t.Fatal("expected event to be delivered")
	}
}
