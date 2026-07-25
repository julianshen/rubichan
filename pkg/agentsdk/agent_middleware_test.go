package agentsdk

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingMiddleware appends its label to a shared trace when entered and
// when the inner handler returns, so tests can assert nesting order.
func recordingMiddleware(trace *traceLog, label string) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, tc ToolCall) Result {
			trace.add(label + ":before")
			res := next(ctx, tc)
			trace.add(label + ":after")
			return res
		}
	}
}

// traceLog is a mutex-guarded ordered log; tools may execute off the turn
// goroutine, so middleware writes are synchronized.
type traceLog struct {
	mu      sync.Mutex
	entries []string
}

func (l *traceLog) add(entry string) {
	l.mu.Lock()
	l.entries = append(l.entries, entry)
	l.mu.Unlock()
}

func (l *traceLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.entries...)
}

func TestAgentToolMiddlewareWrapsExecution(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "echo", `{"text":"hi"}`),
		textResponse("done"),
	}}
	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	trace := &traceLog{}
	a := NewAgent(p, WithTools(r), WithToolMiddlewares(
		recordingMiddleware(trace, "outer"),
		recordingMiddleware(trace, "inner"),
	))

	ch, err := a.Turn(context.Background(), "use echo")
	require.NoError(t, err)
	for range ch {
	}

	// The first registered middleware is the outermost wrapper.
	assert.Equal(t, []string{
		"outer:before", "inner:before", "inner:after", "outer:after",
	}, trace.all())
}

func TestAgentToolMiddlewareReceivesToolCall(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_42", "echo", `{"text":"hi"}`),
		textResponse("done"),
	}}
	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	var seen ToolCall
	var mu sync.Mutex
	capture := func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, tc ToolCall) Result {
			mu.Lock()
			seen = tc
			mu.Unlock()
			return next(ctx, tc)
		}
	}

	a := NewAgent(p, WithTools(r), WithToolMiddlewares(capture))
	ch, err := a.Turn(context.Background(), "use echo")
	require.NoError(t, err)
	for range ch {
	}

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "tc_42", seen.ID)
	assert.Equal(t, "echo", seen.Name)
	assert.JSONEq(t, `{"text":"hi"}`, string(seen.Input))
}

func TestAgentToolMiddlewareCanTransformResult(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "echo", `{"text":"hi"}`),
		textResponse("done"),
	}}
	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	redact := func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, tc ToolCall) Result {
			res := next(ctx, tc)
			res.Content = "[redacted]"
			return res
		}
	}

	a := NewAgent(p, WithTools(r), WithToolMiddlewares(redact))
	ch, err := a.Turn(context.Background(), "use echo")
	require.NoError(t, err)

	var results []TurnEvent
	for ev := range ch {
		if ev.Type == "tool_result" {
			results = append(results, ev)
		}
	}

	require.Len(t, results, 1)
	assert.Equal(t, "[redacted]", results[0].ToolResult.Content,
		"middleware must be able to rewrite the result the conversation sees")
}

func TestAgentToolMiddlewareCanBlockExecution(t *testing.T) {
	// A policy middleware that never calls next must prevent the tool from
	// running at all — the whole point of a gating middleware.
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "counted", `{}`),
		textResponse("done"),
	}}
	r := NewRegistry()
	counter := &countingTool{}
	require.NoError(t, r.Register(counter))

	block := func(HandlerFunc) HandlerFunc {
		return func(context.Context, ToolCall) Result {
			return Result{Content: "blocked by policy", IsError: true}
		}
	}

	a := NewAgent(p, WithTools(r), WithToolMiddlewares(block))
	ch, err := a.Turn(context.Background(), "go")
	require.NoError(t, err)

	var results []TurnEvent
	for ev := range ch {
		if ev.Type == "tool_result" {
			results = append(results, ev)
		}
	}

	require.Len(t, results, 1)
	assert.True(t, results[0].ToolResult.IsError)
	assert.Equal(t, "blocked by policy", results[0].ToolResult.Content)
	assert.Equal(t, 0, counter.count(), "a blocking middleware must stop the tool from executing")
}

func TestAgentToolMiddlewareNotReachedWhenApprovalDenies(t *testing.T) {
	// Denial short-circuits before execution, so middlewares — which wrap
	// execution — must not run for a denied call.
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "echo", `{}`),
		textResponse("done"),
	}}
	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	trace := &traceLog{}
	denyAll := func(context.Context, string, json.RawMessage) (bool, error) { return false, nil }

	a := NewAgent(p, WithTools(r), WithApproval(denyAll),
		WithToolMiddlewares(recordingMiddleware(trace, "mw")))

	ch, err := a.Turn(context.Background(), "go")
	require.NoError(t, err)
	for range ch {
	}

	assert.Empty(t, trace.all(), "a denied tool call must never reach the middleware pipeline")
}

func TestAgentToolMiddlewarePreservesStreaming(t *testing.T) {
	// Routing execution through the pipeline must not break progress events
	// from a StreamingTool.
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "stream_echo", `{"text":"hello"}`),
		textResponse("done"),
	}}
	r := NewRegistry()
	require.NoError(t, r.Register(&streamEchoTool{}))

	trace := &traceLog{}
	a := NewAgent(p, WithTools(r), WithToolMiddlewares(recordingMiddleware(trace, "mw")))

	ch, err := a.Turn(context.Background(), "go")
	require.NoError(t, err)

	var progress, results []TurnEvent
	for ev := range ch {
		switch ev.Type {
		case "tool_progress":
			progress = append(progress, ev)
		case "tool_result":
			results = append(results, ev)
		}
	}

	require.Len(t, progress, 1, "streaming progress must survive the pipeline")
	assert.Equal(t, "streaming...", progress[0].ToolProgress.Content)
	require.Len(t, results, 1)
	assert.Equal(t, "hello", results[0].ToolResult.Content)
	assert.NotEmpty(t, trace.all(), "the middleware still wrapped the streaming execution")
}

func TestAgentToolMiddlewareNilIgnored(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "echo", `{"text":"hi"}`),
		textResponse("done"),
	}}
	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	trace := &traceLog{}
	// A nil middleware interleaved with a real one must not panic.
	a := NewAgent(p, WithTools(r), WithToolMiddlewares(nil, recordingMiddleware(trace, "mw")))

	ch, err := a.Turn(context.Background(), "go")
	require.NoError(t, err)
	for range ch {
	}

	assert.Equal(t, []string{"mw:before", "mw:after"}, trace.all())
}

func TestAgentNoToolMiddlewaresLeavesExecutionUnchanged(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "echo", `{"text":"hi"}`),
		textResponse("done"),
	}}
	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	a := NewAgent(p, WithTools(r))
	ch, err := a.Turn(context.Background(), "go")
	require.NoError(t, err)

	var results []TurnEvent
	for ev := range ch {
		if ev.Type == "tool_result" {
			results = append(results, ev)
		}
	}

	require.Len(t, results, 1)
	assert.False(t, results[0].ToolResult.IsError)
	assert.JSONEq(t, `{"text":"hi"}`, results[0].ToolResult.Content,
		"with no middlewares the tool output must pass through unchanged")
}

// countingTool records how many times it was executed.
type countingTool struct {
	mu sync.Mutex
	n  int
}

func (c *countingTool) Name() string                 { return "counted" }
func (c *countingTool) Description() string          { return "counts executions" }
func (c *countingTool) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (c *countingTool) Execute(context.Context, json.RawMessage) (ToolResult, error) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return ToolResult{Content: "ran"}, nil
}

func (c *countingTool) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}
