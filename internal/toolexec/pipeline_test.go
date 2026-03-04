package toolexec_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/stretchr/testify/assert"
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
