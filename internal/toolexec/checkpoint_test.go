package toolexec_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/checkpoint"
	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckpointMiddlewareCaptures(t *testing.T) {
	rootDir := t.TempDir()
	testFile := filepath.Join(rootDir, "test.go")
	os.WriteFile(testFile, []byte("original"), 0644)

	mgr, err := checkpoint.New(rootDir, "mw-test", 0)
	require.NoError(t, err)
	defer func() { _ = mgr.Cleanup() }()

	turn := 1
	mw := toolexec.CheckpointMiddleware(mgr, func() int { return turn })

	called := false
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		called = true
		return toolexec.Result{Content: "ok"}
	}

	handler := mw(base)
	input, _ := json.Marshal(map[string]string{"operation": "write", "path": "test.go"})
	result := handler(context.Background(), toolexec.ToolCall{Name: "file", Input: input})

	assert.True(t, called)
	assert.Equal(t, "ok", result.Content)
	assert.Len(t, mgr.List(), 1, "should have captured a checkpoint")
}

func TestCheckpointMiddlewareSkipsRead(t *testing.T) {
	rootDir := t.TempDir()
	mgr, err := checkpoint.New(rootDir, "mw-read", 0)
	require.NoError(t, err)
	defer func() { _ = mgr.Cleanup() }()

	mw := toolexec.CheckpointMiddleware(mgr, func() int { return 1 })
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "ok"}
	}

	handler := mw(base)
	input, _ := json.Marshal(map[string]string{"operation": "read", "path": "test.go"})
	handler(context.Background(), toolexec.ToolCall{Name: "file", Input: input})

	assert.Empty(t, mgr.List(), "read should not capture checkpoint")
}

func TestCheckpointMiddlewareSkipsNonFile(t *testing.T) {
	rootDir := t.TempDir()
	mgr, err := checkpoint.New(rootDir, "mw-nonfile", 0)
	require.NoError(t, err)
	defer func() { _ = mgr.Cleanup() }()

	mw := toolexec.CheckpointMiddleware(mgr, func() int { return 1 })
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "ok"}
	}

	handler := mw(base)
	handler(context.Background(), toolexec.ToolCall{Name: "shell", Input: json.RawMessage(`{}`)})

	assert.Empty(t, mgr.List(), "non-file tool should not capture checkpoint")
}

func TestCheckpointMiddlewareNilPassthrough(t *testing.T) {
	mw := toolexec.CheckpointMiddleware(nil, nil)
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "ok"}
	}

	handler := mw(base)
	result := handler(context.Background(), toolexec.ToolCall{Name: "file", Input: json.RawMessage(`{}`)})
	assert.Equal(t, "ok", result.Content)
}

func TestCheckpointMiddlewarePatchOperation(t *testing.T) {
	rootDir := t.TempDir()
	testFile := filepath.Join(rootDir, "patch.go")
	os.WriteFile(testFile, []byte("before"), 0644)

	mgr, err := checkpoint.New(rootDir, "mw-patch", 0)
	require.NoError(t, err)
	defer func() { _ = mgr.Cleanup() }()

	mw := toolexec.CheckpointMiddleware(mgr, func() int { return 2 })
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "ok"}
	}

	handler := mw(base)
	input, _ := json.Marshal(map[string]string{"operation": "patch", "path": "patch.go"})
	handler(context.Background(), toolexec.ToolCall{Name: "file", Input: input})

	cps := mgr.List()
	require.Len(t, cps, 1)
	assert.Equal(t, "patch", cps[0].Operation)
	assert.Equal(t, 2, cps[0].Turn)
}
