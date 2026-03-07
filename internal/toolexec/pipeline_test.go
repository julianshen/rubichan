package toolexec_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPipelineExecutesBaseHandler(t *testing.T) {
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		assert.Equal(t, "call-1", tc.ID)
		assert.Equal(t, "read_file", tc.Name)
		assert.JSONEq(t, `{"path":"/tmp/test.go"}`, string(tc.Input))

		return toolexec.Result{
			Content:        "file contents here",
			DisplayContent: "Read /tmp/test.go",
			IsError:        false,
		}
	}

	p := toolexec.NewPipeline(base)
	result := p.Execute(context.Background(), toolexec.ToolCall{
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

	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		order = append(order, "base")
		return toolexec.Result{Content: "base-result"}
	}

	first := func(next toolexec.HandlerFunc) toolexec.HandlerFunc {
		return func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
			order = append(order, "first:before")
			result := next(ctx, tc)
			order = append(order, "first:after")
			return result
		}
	}

	second := func(next toolexec.HandlerFunc) toolexec.HandlerFunc {
		return func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
			order = append(order, "second:before")
			result := next(ctx, tc)
			order = append(order, "second:after")
			return result
		}
	}

	p := toolexec.NewPipeline(base, first, second)
	result := p.Execute(context.Background(), toolexec.ToolCall{
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

	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		baseCalled = true
		return toolexec.Result{Content: "should not reach"}
	}

	blocker := func(next toolexec.HandlerFunc) toolexec.HandlerFunc {
		return func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
			// Short-circuit: do not call next
			return toolexec.Result{
				Content: "blocked",
				IsError: true,
			}
		}
	}

	p := toolexec.NewPipeline(base, blocker)
	result := p.Execute(context.Background(), toolexec.ToolCall{
		ID:   "call-3",
		Name: "dangerous_tool",
	})

	assert.False(t, baseCalled, "base handler should not be called when middleware short-circuits")
	assert.Equal(t, "blocked", result.Content)
	assert.True(t, result.IsError)
}

func TestPipelineExecuteStreamFinalResult(t *testing.T) {
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "sync result", DisplayContent: "display"}
	}

	p := toolexec.NewPipeline(base)
	ch := p.ExecuteStream(context.Background(), toolexec.ToolCall{
		ID: "call-1", Name: "test",
	})

	var events []toolexec.StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	require.Len(t, events, 1)
	assert.Equal(t, toolexec.StreamFinal, events[0].Type)
	require.NotNil(t, events[0].Result)
	assert.Equal(t, "sync result", events[0].Result.Content)
	assert.Equal(t, "display", events[0].Result.DisplayContent)
}

func TestPipelineExecuteStreamWithProgressEvents(t *testing.T) {
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		emit := tools.EmitterFromContext(ctx)
		if emit != nil {
			emit(tools.ToolEvent{Stage: tools.EventBegin, Content: "starting"})
			emit(tools.ToolEvent{Stage: tools.EventDelta, Content: "line 1\n"})
			emit(tools.ToolEvent{Stage: tools.EventDelta, Content: "line 2\n"})
			emit(tools.ToolEvent{Stage: tools.EventEnd, Content: "done"})
		}
		return toolexec.Result{Content: "final"}
	}

	p := toolexec.NewPipeline(base)
	ch := p.ExecuteStream(context.Background(), toolexec.ToolCall{
		ID: "call-2", Name: "streaming_test",
	})

	var events []toolexec.StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// 4 progress events + 1 final
	require.Len(t, events, 5)
	assert.Equal(t, toolexec.StreamProgress, events[0].Type)
	assert.Equal(t, "starting", events[0].Event.Content)
	assert.Equal(t, toolexec.StreamProgress, events[1].Type)
	assert.Equal(t, "line 1\n", events[1].Event.Content)
	assert.Equal(t, toolexec.StreamProgress, events[2].Type)
	assert.Equal(t, toolexec.StreamFinal, events[4].Type)
	assert.Equal(t, "final", events[4].Result.Content)
}

func TestPipelineExecuteStreamMiddlewarePreservesEmitter(t *testing.T) {
	var progressReceived bool
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		emit := tools.EmitterFromContext(ctx)
		if emit != nil {
			emit(tools.ToolEvent{Stage: tools.EventDelta, Content: "progress"})
			progressReceived = true
		}
		return toolexec.Result{Content: "done"}
	}

	wrapper := func(next toolexec.HandlerFunc) toolexec.HandlerFunc {
		return func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
			return next(ctx, tc)
		}
	}

	p := toolexec.NewPipeline(base, wrapper)
	ch := p.ExecuteStream(context.Background(), toolexec.ToolCall{
		ID: "call-3", Name: "test",
	})

	var events []toolexec.StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	assert.True(t, progressReceived)
	require.Len(t, events, 2)
	assert.Equal(t, toolexec.StreamProgress, events[0].Type)
	assert.Equal(t, toolexec.StreamFinal, events[1].Type)
}
