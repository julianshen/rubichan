package toolexec_test

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/evaluator"
	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/stretchr/testify/assert"
)

func TestVerdictMiddlewarePassesThroughUnwatchedTools(t *testing.T) {
	t.Parallel()
	middleware := toolexec.VerdictMiddleware(
		evaluator.DefaultCheckerPipeline(),
		"shell", // Only watch shell
	)

	handler := middleware(func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "original output", IsError: false}
	})

	result := handler(context.Background(), toolexec.ToolCall{
		ID:    "1",
		Name:  "read_file", // Not in watch list
		Input: nil,
	})

	// Content should not contain evaluation
	assert.Equal(t, "original output", result.Content)
	assert.NotContains(t, result.Content, "[evaluation]")
}

func TestVerdictMiddlewareAppendsVerdictForWatchedTool(t *testing.T) {
	t.Parallel()
	middleware := toolexec.VerdictMiddleware(
		evaluator.DefaultCheckerPipeline(),
		"shell",
	)

	handler := middleware(func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "success output", IsError: false}
	})

	result := handler(context.Background(), toolexec.ToolCall{
		ID:    "1",
		Name:  "shell",
		Input: nil,
	})

	// Content should contain evaluation
	assert.Contains(t, result.Content, "success output")
	assert.Contains(t, result.Content, "[evaluation]")
	assert.Contains(t, result.Content, "status=success")
}

func TestVerdictMiddlewareNilPipelinePassesThrough(t *testing.T) {
	t.Parallel()
	middleware := toolexec.VerdictMiddleware(nil, "shell")

	handler := middleware(func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "original output", IsError: false}
	})

	result := handler(context.Background(), toolexec.ToolCall{
		ID:    "1",
		Name:  "shell",
		Input: nil,
	})

	// Content should not be modified when pipeline is nil
	assert.Equal(t, "original output", result.Content)
}

func TestVerdictMiddlewareEmptyWatchListPassesThrough(t *testing.T) {
	t.Parallel()
	middleware := toolexec.VerdictMiddleware(evaluator.DefaultCheckerPipeline()) // No watched tools

	handler := middleware(func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "original output", IsError: false}
	})

	result := handler(context.Background(), toolexec.ToolCall{
		ID:    "1",
		Name:  "shell",
		Input: nil,
	})

	// Content should not be modified when watch list is empty
	assert.Equal(t, "original output", result.Content)
}

func TestVerdictMiddlewarePreservesOriginalContent(t *testing.T) {
	t.Parallel()
	middleware := toolexec.VerdictMiddleware(
		evaluator.DefaultCheckerPipeline(),
		"shell",
	)

	originalOutput := "this is my original output\nwith multiple lines"
	handler := middleware(func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: originalOutput, IsError: false}
	})

	result := handler(context.Background(), toolexec.ToolCall{
		ID:    "1",
		Name:  "shell",
		Input: nil,
	})

	// Original content should be preserved
	assert.Contains(t, result.Content, originalOutput)
	// Verdict should be appended after
	parts := []string{originalOutput, "[evaluation]"}
	for _, part := range parts {
		assert.Contains(t, result.Content, part)
	}
}

func TestVerdictMiddlewareFormatting(t *testing.T) {
	t.Parallel()
	middleware := toolexec.VerdictMiddleware(
		evaluator.DefaultCheckerPipeline(),
		"shell",
	)

	handler := middleware(func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "success", IsError: false}
	})

	result := handler(context.Background(), toolexec.ToolCall{
		ID:    "1",
		Name:  "shell",
		Input: nil,
	})

	// Verdict should be formatted with proper structure
	assert.Contains(t, result.Content, "[evaluation]")
	assert.Contains(t, result.Content, "status=")
	assert.Contains(t, result.Content, "confidence=")
	assert.Contains(t, result.Content, "reason:")
}

func TestVerdictMiddlewareMultipleWatchedTools(t *testing.T) {
	t.Parallel()
	middleware := toolexec.VerdictMiddleware(
		evaluator.DefaultCheckerPipeline(),
		"shell", "write_file", "patch_file",
	)

	handler := middleware(func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "output", IsError: false}
	})

	// Test each watched tool
	for _, toolName := range []string{"shell", "write_file", "patch_file"} {
		result := handler(context.Background(), toolexec.ToolCall{
			ID:    "1",
			Name:  toolName,
			Input: nil,
		})
		assert.Contains(t, result.Content, "[evaluation]", "Tool %s should have verdict", toolName)
	}

	// Test unwatched tool
	result := handler(context.Background(), toolexec.ToolCall{
		ID:    "1",
		Name:  "read_file",
		Input: nil,
	})
	assert.NotContains(t, result.Content, "[evaluation]", "Unwatched tool should not have verdict")
}
