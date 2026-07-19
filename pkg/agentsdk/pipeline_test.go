package agentsdk

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPipelineExecutesBaseHandler(t *testing.T) {
	base := func(ctx context.Context, tc ToolCall) Result {
		assert.Equal(t, "call-1", tc.ID)
		assert.Equal(t, "read_file", tc.Name)
		assert.JSONEq(t, `{"path":"/tmp/test.go"}`, string(tc.Input))

		return Result{
			Content:        "file contents here",
			DisplayContent: "Read /tmp/test.go",
			IsError:        false,
		}
	}

	p := NewPipeline(base)
	result := p.Execute(context.Background(), ToolCall{
		ID:    "call-1",
		Name:  "read_file",
		Input: json.RawMessage(`{"path":"/tmp/test.go"}`),
	})

	assert.Equal(t, "file contents here", result.Content)
	assert.Equal(t, "Read /tmp/test.go", result.DisplayContent)
	assert.False(t, result.IsError)
}

func TestPipelineMiddlewareOrder(t *testing.T) {
	var order []string

	base := func(ctx context.Context, tc ToolCall) Result {
		order = append(order, "base")
		return Result{Content: "base-result"}
	}

	first := func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, tc ToolCall) Result {
			order = append(order, "first:before")
			result := next(ctx, tc)
			order = append(order, "first:after")
			return result
		}
	}

	second := func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, tc ToolCall) Result {
			order = append(order, "second:before")
			result := next(ctx, tc)
			order = append(order, "second:after")
			return result
		}
	}

	p := NewPipeline(base, first, second)
	result := p.Execute(context.Background(), ToolCall{
		ID:   "call-2",
		Name: "test_tool",
	})

	assert.Equal(t, "base-result", result.Content)
	assert.Equal(t, []string{
		"first:before",
		"second:before",
		"base",
		"second:after",
		"first:after",
	}, order)
}

func TestPipelineMiddlewareShortCircuit(t *testing.T) {
	baseCalled := false

	base := func(ctx context.Context, tc ToolCall) Result {
		baseCalled = true
		return Result{Content: "should not reach"}
	}

	blocker := func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, tc ToolCall) Result {
			// Short-circuit: do not call next
			return Result{
				Content: "blocked",
				IsError: true,
			}
		}
	}

	p := NewPipeline(base, blocker)
	result := p.Execute(context.Background(), ToolCall{
		ID:   "call-3",
		Name: "dangerous_tool",
	})

	assert.False(t, baseCalled, "base handler should not be called when middleware short-circuits")
	assert.Equal(t, "blocked", result.Content)
	assert.True(t, result.IsError)
}

func TestPipelineExecuteStreamFinalResult(t *testing.T) {
	base := func(ctx context.Context, tc ToolCall) Result {
		return Result{Content: "sync result", DisplayContent: "display"}
	}

	p := NewPipeline(base)
	ch := p.ExecuteStream(context.Background(), ToolCall{
		ID: "call-1", Name: "test",
	})

	var events []PipelineEvent
	for ev := range ch {
		events = append(events, ev)
	}

	require.Len(t, events, 1)
	assert.Equal(t, PipelineFinal, events[0].Type)
	require.NotNil(t, events[0].Result)
	assert.Equal(t, "sync result", events[0].Result.Content)
	assert.Equal(t, "display", events[0].Result.DisplayContent)
}

func TestPipelineExecuteStreamWithProgressEvents(t *testing.T) {
	base := func(ctx context.Context, tc ToolCall) Result {
		emit := ToolEventEmitterFromContext(ctx)
		if emit != nil {
			emit(ToolEvent{Stage: EventBegin, Content: "starting"})
			emit(ToolEvent{Stage: EventDelta, Content: "line 1\n"})
			emit(ToolEvent{Stage: EventDelta, Content: "line 2\n"})
			emit(ToolEvent{Stage: EventEnd, Content: "done"})
		}
		return Result{Content: "final"}
	}

	p := NewPipeline(base)
	ch := p.ExecuteStream(context.Background(), ToolCall{
		ID: "call-2", Name: "streaming_test",
	})

	var events []PipelineEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// 4 progress events + 1 final
	require.Len(t, events, 5)
	assert.Equal(t, PipelineProgress, events[0].Type)
	assert.Equal(t, "starting", events[0].Event.Content)
	assert.Equal(t, PipelineProgress, events[1].Type)
	assert.Equal(t, "line 1\n", events[1].Event.Content)
	assert.Equal(t, PipelineProgress, events[2].Type)
	assert.Equal(t, PipelineFinal, events[4].Type)
	assert.Equal(t, "final", events[4].Result.Content)
}

func TestPipelineExecuteStreamMiddlewarePreservesEmitter(t *testing.T) {
	var progressReceived bool
	base := func(ctx context.Context, tc ToolCall) Result {
		emit := ToolEventEmitterFromContext(ctx)
		if emit != nil {
			emit(ToolEvent{Stage: EventDelta, Content: "progress"})
			progressReceived = true
		}
		return Result{Content: "done"}
	}

	wrapper := func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, tc ToolCall) Result {
			return next(ctx, tc)
		}
	}

	p := NewPipeline(base, wrapper)
	ch := p.ExecuteStream(context.Background(), ToolCall{
		ID: "call-3", Name: "test",
	})

	var events []PipelineEvent
	for ev := range ch {
		events = append(events, ev)
	}

	assert.True(t, progressReceived)
	require.Len(t, events, 2)
	assert.Equal(t, PipelineProgress, events[0].Type)
	assert.Equal(t, PipelineFinal, events[1].Type)
}

func TestToolEventEmitterFromContextNilWhenAbsent(t *testing.T) {
	assert.Nil(t, ToolEventEmitterFromContext(context.Background()))
}

func TestWithToolEventEmitterNilEmitNoop(t *testing.T) {
	ctx := WithToolEventEmitter(context.Background(), nil)
	assert.Nil(t, ToolEventEmitterFromContext(ctx))
}
