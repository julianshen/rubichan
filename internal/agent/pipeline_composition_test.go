package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/checkpoint"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/store"
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

// oversizedShellTool returns output larger than the offload threshold with
// an error pattern buried past the offload preview (first 200 chars).
type oversizedShellTool struct{ output string }

func (t oversizedShellTool) Name() string        { return "shell" }
func (t oversizedShellTool) Description() string { return "stub shell" }
func (t oversizedShellTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (t oversizedShellTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{Content: t.output}, nil
}

// TestVerdictEvaluatesRealContentBeforeOffload pins the middleware wrapper
// order for verdict vs. output offloading: post-next work runs innermost
// first, so VerdictMiddleware must be registered after (inside)
// OutputManagerMiddleware. Otherwise the offloader replaces oversized
// content with a read_result reference whose 200-char preview hides late
// error patterns, and the evaluator stamps success on failed tool runs.
func TestVerdictEvaluatesRealContentBeforeOffload(t *testing.T) {
	st, err := store.NewStore(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	// >4096 bytes (default offload threshold), error pattern well past the
	// 200-char offload preview so only the real content reveals it.
	output := strings.Repeat("build log line\n", 300) + "\nfatal error: all goroutines are asleep"
	require.Greater(t, len(output), 4096)

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(oversizedShellTool{output: output}))

	cfg := config.DefaultConfig()
	a := New(&mockProvider{}, reg, autoApprove, cfg, WithStore(st))
	require.NotNil(t, a.resultStore, "store-backed agent must have a result store")

	result := a.pipeline.Execute(context.Background(), toolexec.ToolCall{
		ID: "tc-v", Name: "shell", Input: json.RawMessage(`{"command":"make"}`),
	})

	// Oversized output must end up offloaded to a reference.
	require.Contains(t, result.Content, "read_result", "oversized output should be offloaded")

	// The verdict must have been computed from the real content: the stored
	// blob carries the evaluation, and it must not report success given the
	// buried fatal error.
	refMatch := regexp.MustCompile(`ref_id="([^"]+)"`).FindStringSubmatch(result.Content)
	require.NotNil(t, refMatch, "offload reference id must be present: %s", result.Content)
	blob, err := a.resultStore.Retrieve(refMatch[1])
	require.NoError(t, err)
	assert.Contains(t, blob, "[evaluation]", "verdict must be evaluated before offloading")
	// The evidence line is the proof the evaluator saw the real content:
	// the pattern sits past the 200-char offload preview, so grading the
	// reference could never surface it. (Whether a detected pattern flips
	// the overall status is evaluator policy, out of scope here.)
	assert.Contains(t, blob, `detected error pattern: "fatal error"`,
		"verdict must be computed from the full pre-offload output")
	assert.Contains(t, blob, "fatal error: all goroutines", "blob must hold the real output")
}
