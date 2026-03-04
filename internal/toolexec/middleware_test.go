package toolexec_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/stretchr/testify/assert"
)

// --- Mock implementations ---

type mockHookDispatcher struct {
	beforeCancel bool
	beforeErr    error
	afterMod     map[string]any
	afterErr     error

	beforeCalled bool
	afterCalled  bool
	beforeName   string
	afterName    string
	afterContent string
	afterIsError bool
}

func (m *mockHookDispatcher) DispatchBeforeToolCall(_ context.Context, toolName string, _ json.RawMessage) (bool, error) {
	m.beforeCalled = true
	m.beforeName = toolName
	return m.beforeCancel, m.beforeErr
}

func (m *mockHookDispatcher) DispatchAfterToolResult(_ context.Context, toolName, content string, isError bool) (map[string]any, error) {
	m.afterCalled = true
	m.afterName = toolName
	m.afterContent = content
	m.afterIsError = isError
	return m.afterMod, m.afterErr
}

type mockOutputOffloader struct {
	returnRef string
	returnErr error

	calledName    string
	calledID      string
	calledContent string
	called        bool
}

func (m *mockOutputOffloader) OffloadResult(toolName, toolUseID, content string) (string, error) {
	m.called = true
	m.calledName = toolName
	m.calledID = toolUseID
	m.calledContent = content
	return m.returnRef, m.returnErr
}

// --- HookMiddleware tests ---

func TestHookMiddlewareCancels(t *testing.T) {
	dispatcher := &mockHookDispatcher{beforeCancel: true}
	baseCalled := false
	base := func(_ context.Context, _ toolexec.ToolCall) toolexec.Result {
		baseCalled = true
		return toolexec.Result{Content: "should not reach"}
	}

	mw := toolexec.HookMiddleware(dispatcher)
	handler := mw(base)
	result := handler(context.Background(), toolexec.ToolCall{
		ID:   "call-1",
		Name: "write_file",
	})

	assert.False(t, baseCalled, "base handler should not be called when hook cancels")
	assert.Equal(t, "tool call cancelled by skill", result.Content)
	assert.True(t, result.IsError)
	assert.True(t, dispatcher.beforeCalled)
	assert.Equal(t, "write_file", dispatcher.beforeName)
}

func TestHookMiddlewarePassesThrough(t *testing.T) {
	dispatcher := &mockHookDispatcher{beforeCancel: false}
	baseCalled := false
	base := func(_ context.Context, _ toolexec.ToolCall) toolexec.Result {
		baseCalled = true
		return toolexec.Result{Content: "base result", DisplayContent: "display"}
	}

	mw := toolexec.HookMiddleware(dispatcher)
	handler := mw(base)
	result := handler(context.Background(), toolexec.ToolCall{
		ID:   "call-2",
		Name: "read_file",
	})

	assert.True(t, baseCalled, "base handler should be called when hook does not cancel")
	assert.Equal(t, "base result", result.Content)
	assert.Equal(t, "display", result.DisplayContent)
	assert.False(t, result.IsError)
}

func TestHookMiddlewareNilDispatcher(t *testing.T) {
	baseCalled := false
	base := func(_ context.Context, _ toolexec.ToolCall) toolexec.Result {
		baseCalled = true
		return toolexec.Result{Content: "pass through"}
	}

	mw := toolexec.HookMiddleware(nil)
	handler := mw(base)
	result := handler(context.Background(), toolexec.ToolCall{
		ID:   "call-3",
		Name: "some_tool",
	})

	assert.True(t, baseCalled)
	assert.Equal(t, "pass through", result.Content)
	assert.False(t, result.IsError)
}

func TestHookMiddlewareError(t *testing.T) {
	dispatcher := &mockHookDispatcher{beforeErr: errors.New("hook failed")}
	baseCalled := false
	base := func(_ context.Context, _ toolexec.ToolCall) toolexec.Result {
		baseCalled = true
		return toolexec.Result{Content: "should not reach"}
	}

	mw := toolexec.HookMiddleware(dispatcher)
	handler := mw(base)
	result := handler(context.Background(), toolexec.ToolCall{
		ID:   "call-4",
		Name: "write_file",
	})

	assert.False(t, baseCalled, "base handler should not be called when hook errors")
	assert.Equal(t, "hook error: hook failed", result.Content)
	assert.True(t, result.IsError)
}

// --- PostHookMiddleware tests ---

func TestPostHookMiddlewareModifiesContent(t *testing.T) {
	dispatcher := &mockHookDispatcher{
		afterMod: map[string]any{"content": "modified content"},
	}
	base := func(_ context.Context, _ toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{
			Content:        "original content",
			DisplayContent: "original display",
			IsError:        false,
		}
	}

	mw := toolexec.PostHookMiddleware(dispatcher)
	handler := mw(base)
	result := handler(context.Background(), toolexec.ToolCall{
		ID:   "call-5",
		Name: "read_file",
	})

	assert.Equal(t, "modified content", result.Content)
	assert.Empty(t, result.DisplayContent, "DisplayContent should be cleared when content is modified")
	assert.False(t, result.IsError)
	assert.True(t, dispatcher.afterCalled)
	assert.Equal(t, "read_file", dispatcher.afterName)
	assert.Equal(t, "original content", dispatcher.afterContent)
}

func TestPostHookMiddlewareNoModification(t *testing.T) {
	dispatcher := &mockHookDispatcher{afterMod: nil}
	base := func(_ context.Context, _ toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{
			Content:        "unchanged",
			DisplayContent: "display unchanged",
			IsError:        false,
		}
	}

	mw := toolexec.PostHookMiddleware(dispatcher)
	handler := mw(base)
	result := handler(context.Background(), toolexec.ToolCall{
		ID:   "call-6",
		Name: "shell",
	})

	assert.Equal(t, "unchanged", result.Content)
	assert.Equal(t, "display unchanged", result.DisplayContent)
	assert.False(t, result.IsError)
}

func TestPostHookMiddlewareErrorGracefulDegradation(t *testing.T) {
	dispatcher := &mockHookDispatcher{afterErr: errors.New("post-hook failed")}
	base := func(_ context.Context, _ toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{
			Content:        "original",
			DisplayContent: "display",
			IsError:        false,
		}
	}

	mw := toolexec.PostHookMiddleware(dispatcher)
	handler := mw(base)
	result := handler(context.Background(), toolexec.ToolCall{
		ID:   "call-7",
		Name: "read_file",
	})

	// Graceful degradation: error doesn't change result.
	assert.Equal(t, "original", result.Content)
	assert.Equal(t, "display", result.DisplayContent)
	assert.False(t, result.IsError)
}

func TestPostHookMiddlewareNilDispatcher(t *testing.T) {
	base := func(_ context.Context, _ toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "pass through"}
	}

	mw := toolexec.PostHookMiddleware(nil)
	handler := mw(base)
	result := handler(context.Background(), toolexec.ToolCall{
		ID:   "call-8",
		Name: "tool",
	})

	assert.Equal(t, "pass through", result.Content)
}

// --- OutputManagerMiddleware tests ---

func TestOutputManagerMiddlewareOffloadsLargeResults(t *testing.T) {
	offloader := &mockOutputOffloader{returnRef: "[result stored: ref-abc123]"}
	base := func(_ context.Context, _ toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{
			Content:        "very large content that should be offloaded",
			DisplayContent: "display content",
			IsError:        false,
		}
	}

	mw := toolexec.OutputManagerMiddleware(offloader)
	handler := mw(base)
	result := handler(context.Background(), toolexec.ToolCall{
		ID:   "call-9",
		Name: "shell",
	})

	assert.Equal(t, "[result stored: ref-abc123]", result.Content)
	assert.True(t, offloader.called)
	assert.Equal(t, "shell", offloader.calledName)
	assert.Equal(t, "call-9", offloader.calledID)
	assert.Equal(t, "very large content that should be offloaded", offloader.calledContent)
}

func TestOutputManagerMiddlewareSkipsErrors(t *testing.T) {
	offloader := &mockOutputOffloader{returnRef: "should not appear"}
	base := func(_ context.Context, _ toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{
			Content: "error content",
			IsError: true,
		}
	}

	mw := toolexec.OutputManagerMiddleware(offloader)
	handler := mw(base)
	result := handler(context.Background(), toolexec.ToolCall{
		ID:   "call-10",
		Name: "write_file",
	})

	assert.False(t, offloader.called, "offloader should not be called for error results")
	assert.Equal(t, "error content", result.Content)
	assert.True(t, result.IsError)
}

func TestOutputManagerMiddlewareNilOffloader(t *testing.T) {
	base := func(_ context.Context, _ toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "pass through"}
	}

	mw := toolexec.OutputManagerMiddleware(nil)
	handler := mw(base)
	result := handler(context.Background(), toolexec.ToolCall{
		ID:   "call-11",
		Name: "tool",
	})

	assert.Equal(t, "pass through", result.Content)
}

func TestOutputManagerMiddlewareOffloaderError(t *testing.T) {
	offloader := &mockOutputOffloader{returnErr: errors.New("storage full")}
	base := func(_ context.Context, _ toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "original content"}
	}

	mw := toolexec.OutputManagerMiddleware(offloader)
	handler := mw(base)
	result := handler(context.Background(), toolexec.ToolCall{
		ID:   "call-12",
		Name: "shell",
	})

	// On offloader error, original result should be preserved.
	assert.Equal(t, "original content", result.Content)
	assert.False(t, result.IsError)
}
