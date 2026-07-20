package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/checkpoint"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/julianshen/rubichan/internal/tools"
)

// stubFileTool stands in for the real file tool so the pipeline's base
// executor has something to dispatch to.
type stubFileTool struct{}

func (stubFileTool) Name() string                 { return "file" }
func (stubFileTool) Description() string          { return "stub file tool" }
func (stubFileTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (stubFileTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{Content: "ok"}, nil
}

func spyMiddleware(name string, order *[]string) toolexec.Middleware {
	return func(next toolexec.HandlerFunc) toolexec.HandlerFunc {
		return func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
			*order = append(*order, name)
			return next(ctx, tc)
		}
	}
}

// TestProductionShapedWiringCapturesCheckpoints locks the fix for the
// composition drift where main.go's WithPipeline replaced the agent's
// pipeline wholesale, silently dropping CheckpointMiddleware — so /undo
// was wired against a manager that never captured anything. Composition
// now lives in the agent: the root registers only its slot middlewares
// and the agent owns the core chain (hooks, checkpoint, verdict, output).
func TestProductionShapedWiringCapturesCheckpoints(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(target, []byte("package main\n"), 0o644))

	mgr, err := checkpoint.New(dir, "sess-test", 0)
	require.NoError(t, err)

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(stubFileTool{}))

	var order []string
	cfg := config.DefaultConfig()
	a := New(&mockProvider{}, reg, autoApprove, cfg,
		WithCheckpointManager(mgr),
		WithToolMiddlewares(ToolMiddlewares{
			BeforeHooks: []toolexec.Middleware{spyMiddleware("gate", &order)},
			AfterHooks:  []toolexec.Middleware{spyMiddleware("safety", &order)},
		}),
	)

	input, _ := json.Marshal(map[string]string{"operation": "write", "path": target})
	result := a.pipeline.Execute(context.Background(), toolexec.ToolCall{
		ID: "tc-1", Name: "file", Input: input,
	})
	require.False(t, result.IsError, "stub tool execution should succeed: %s", result.Content)

	// The headline assertion: a write through the production-shaped
	// pipeline must capture pre-write file state for undo/rewind.
	cps := mgr.List()
	require.NotEmpty(t, cps, "write must produce a checkpoint capture")

	// Slot ordering: registered middlewares ran, gate before safety.
	assert.Equal(t, []string{"gate", "safety"}, order)
}

// TestDefaultCompositionUnchangedWithoutSlots guards the fallback path:
// with no slot middlewares registered, New composes the same core chain
// as before (hooks → checkpoint → post-hooks → verdict → output), so
// existing consumers see no change.
func TestDefaultCompositionUnchangedWithoutSlots(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.txt")
	require.NoError(t, os.WriteFile(target, []byte("x"), 0o644))

	mgr, err := checkpoint.New(dir, "sess-default", 0)
	require.NoError(t, err)

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(stubFileTool{}))

	cfg := config.DefaultConfig()
	a := New(&mockProvider{}, reg, autoApprove, cfg, WithCheckpointManager(mgr))

	input, _ := json.Marshal(map[string]string{"operation": "patch", "path": target})
	result := a.pipeline.Execute(context.Background(), toolexec.ToolCall{
		ID: "tc-2", Name: "file", Input: input,
	})
	require.False(t, result.IsError)
	require.NotEmpty(t, mgr.List())
}
