package agentsdk

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// drainTypes consumes a turn channel and returns the event types seen.
func drainTypes(ch <-chan TurnEvent) ([]string, []TurnEvent) {
	var types []string
	var events []TurnEvent
	for ev := range ch {
		types = append(types, ev.Type)
		events = append(events, ev)
	}
	return types, events
}

func TestAgentToolMiddlewarePanicContained(t *testing.T) {
	// A panicking middleware runs on the turn goroutine. Without a recover
	// boundary it would take down the host process; it must instead fold
	// into an error result and let the turn continue — matching the
	// containment the ContextStrategy and BackgroundTask seams already have.
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "echo", `{}`),
		textResponse("recovered and continued"),
	}}
	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	logger := &syncLogger{}
	panicMW := func(HandlerFunc) HandlerFunc {
		return func(context.Context, ToolCall) Result { panic("middleware boom") }
	}

	a := NewAgent(p, WithTools(r), WithLogger(logger), WithToolMiddlewares(panicMW))
	ch, err := a.Turn(context.Background(), "go")
	require.NoError(t, err)
	types, events := drainTypes(ch)

	var results []TurnEvent
	for _, ev := range events {
		if ev.Type == "tool_result" {
			results = append(results, ev)
		}
	}
	require.Len(t, results, 1)
	assert.True(t, results[0].ToolResult.IsError)
	assert.Contains(t, results[0].ToolResult.Content, "panic")
	assert.Contains(t, types, "done", "the turn must complete after a middleware panic")

	require.NotEmpty(t, logger.warnings())
	assert.Contains(t, logger.warnings()[0], "panicked")
}

// panickingTool panics when executed.
type panickingTool struct{}

func (panickingTool) Name() string                 { return "panicker" }
func (panickingTool) Description() string          { return "panics" }
func (panickingTool) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (panickingTool) Execute(context.Context, json.RawMessage) (ToolResult, error) {
	panic("tool boom")
}

func TestAgentToolPanicContained(t *testing.T) {
	// ToolExecOutcome documents that a misbehaving tool never aborts the
	// turn; a panic is a misbehaviour like any other.
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "panicker", `{}`),
		textResponse("recovered and continued"),
	}}
	r := NewRegistry()
	require.NoError(t, r.Register(panickingTool{}))

	a := NewAgent(p, WithTools(r), WithLogger(&syncLogger{}))
	ch, err := a.Turn(context.Background(), "go")
	require.NoError(t, err)
	types, events := drainTypes(ch)

	var results []TurnEvent
	for _, ev := range events {
		if ev.Type == "tool_result" {
			results = append(results, ev)
		}
	}
	require.Len(t, results, 1)
	assert.True(t, results[0].ToolResult.IsError)
	assert.Contains(t, results[0].ToolResult.Content, "panic")
	assert.Contains(t, types, "done", "the turn must complete after a tool panic")
}

// panickingProvider panics inside Stream, exercising a failure outside the
// tool-dispatch boundary.
type panickingProvider struct{}

func (panickingProvider) Stream(context.Context, CompletionRequest) (<-chan StreamEvent, error) {
	panic("provider boom")
}

func TestAgentTurnPanicOutsideDispatchContained(t *testing.T) {
	// The turn goroutine is unsupervised: a panic anywhere in the loop that
	// is not caught by a narrower boundary would kill the process. The loop
	// must recover, report, and still close out the turn so a consumer
	// blocked on "done" is not left hanging.
	logger := &syncLogger{}
	a := NewAgent(panickingProvider{}, WithLogger(logger))

	ch, err := a.Turn(context.Background(), "go")
	require.NoError(t, err)
	types, events := drainTypes(ch)

	assert.Contains(t, types, "error", "a recovered panic must surface as an error event")
	assert.Contains(t, types, "done", "the turn must still terminate with done")

	var sawPanicErr bool
	for _, ev := range events {
		if ev.Type == "error" && ev.Error != nil && strings.Contains(ev.Error.Error(), "panic") {
			sawPanicErr = true
		}
	}
	assert.True(t, sawPanicErr, "the error event must identify the panic")
}

func TestAgentTurnPanicReleasesTurnLock(t *testing.T) {
	// The panic recover must not strand turnMu, or every later turn would
	// deadlock.
	a := NewAgent(panickingProvider{}, WithLogger(&syncLogger{}))

	ch, err := a.Turn(context.Background(), "first")
	require.NoError(t, err)
	drainTypes(ch)

	done := make(chan struct{})
	go func() {
		defer close(done)
		ch2, err2 := a.Turn(context.Background(), "second")
		if err2 == nil {
			drainTypes(ch2)
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("second turn blocked — the panic recover stranded the turn lock")
	}
}
