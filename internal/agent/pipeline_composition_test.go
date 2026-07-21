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
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/skills"
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

	// The verdict must be BOTH computed from the real content AND visible
	// inline after offloading. The evidence line is the proof the evaluator
	// saw the real content — the pattern sits past the 200-char offload
	// preview, so grading the reference could never surface it — and the
	// spec requires the verdict in the conversation, so it must survive
	// offloading rather than vanish into the stored blob. (Whether a
	// detected pattern flips the overall status is evaluator policy, out
	// of scope here.)
	assert.Contains(t, result.Content, "[evaluation]",
		"verdict must remain visible in the conversation after offloading")
	assert.Contains(t, result.Content, `detected error pattern: "fatal error"`,
		"inline verdict must be computed from the full pre-offload output")

	// The stored blob holds the raw tool output.
	refMatch := regexp.MustCompile(`ref_id="([^"]+)"`).FindStringSubmatch(result.Content)
	require.NotNil(t, refMatch, "offload reference id must be present: %s", result.Content)
	blob, err := a.resultStore.Retrieve(refMatch[1])
	require.NoError(t, err)
	assert.Contains(t, blob, "fatal error: all goroutines", "blob must hold the real output")
}

// TestVerdictCoversCanonicalFileWrites pins verdict coverage for the
// canonical file tool: models call "file" with operation write/patch —
// the write_file/patch_file names in criticalToolsForEvaluation are
// aliases hidden from Registry.All(), so exact-name matching alone
// evaluates only hallucinated alias calls and misses real file edits.
func TestVerdictCoversCanonicalFileWrites(t *testing.T) {
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(stubFileTool{}))

	cfg := config.DefaultConfig()
	a := New(&mockProvider{}, reg, autoApprove, cfg)

	writeInput, _ := json.Marshal(map[string]string{"operation": "write", "path": "x.go"})
	result := a.pipeline.Execute(context.Background(), toolexec.ToolCall{
		ID: "tc-w", Name: "file", Input: writeInput,
	})
	assert.Contains(t, result.Content, "[evaluation]",
		"canonical file write must be evaluated")

	readInput, _ := json.Marshal(map[string]string{"operation": "read", "path": "x.go"})
	result = a.pipeline.Execute(context.Background(), toolexec.ToolCall{
		ID: "tc-r", Name: "file", Input: readInput,
	})
	assert.NotContains(t, result.Content, "[evaluation]",
		"file reads are not critical operations and must not be evaluated")
}

// TestAfterToolHooksSeeFullResultOnce pins two properties of the
// post-result data flow for store-backed sessions:
//
//  1. OnAfterToolResult hooks receive the full tool output, not the
//     read_result reference the offloader substitutes for oversized
//     results — skills that redact or transform large outputs must
//     operate on the real content (post-next order: hooks → verdict →
//     offload).
//  2. The hook fires exactly once per result. executeSingleTool used to
//     dispatch it inline after the pipeline (post-offload, seeing the
//     stub) on top of the pipeline's own PostHookMiddleware dispatch.
func TestAfterToolHooksSeeFullResultOnce(t *testing.T) {
	var seen []string
	hooks := map[skills.HookPhase]skills.HookHandler{
		skills.HookOnAfterToolResult: func(event skills.HookEvent) (skills.HookResult, error) {
			if c, ok := event.Data[skills.HookDataContent].(string); ok {
				seen = append(seen, c)
			}
			return skills.HookResult{}, nil
		},
	}
	rt := makeTestRuntime(t, "spy-hook", toolManifest("spy-hook"), nil, hooks)

	st, err := store.NewStore(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	output := strings.Repeat("chatty output line\n", 300) + "\nBURIED-MARKER"
	require.Greater(t, len(output), 4096)

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(oversizedShellTool{output: output}))

	cfg := config.DefaultConfig()
	a := New(&mockProvider{}, reg, autoApprove, cfg, WithStore(st), WithSkillRuntime(rt))

	ch := make(chan TurnEvent, 64)
	res := a.executeSingleTool(context.Background(), ch, provider.ToolUseBlock{
		ID: "tc-h", Name: "shell", Input: json.RawMessage(`{"command":"make"}`),
	})
	require.False(t, res.isError)

	require.Len(t, seen, 1, "after-tool-result hook must fire exactly once per result")
	assert.Contains(t, seen[0], "BURIED-MARKER",
		"hook must receive the full pre-offload output, not the offload reference")
	assert.NotContains(t, seen[0], "read_result",
		"hook must not receive the offload stub")
}

// TestAfterToolHooksReceiveToolInput pins the after-result hook payload:
// handlers must receive the original tool input as a string under
// HookDataInput — post_edit filters and {{file}}/{{command}} template
// variables parse it to target completed file/shell calls. (The old
// inline dispatch passed a json.RawMessage, which every consumer's
// string assertion silently rejected — so this contract is load-bearing
// for the first time.)
func TestAfterToolHooksReceiveToolInput(t *testing.T) {
	var gotInput any
	hooks := map[skills.HookPhase]skills.HookHandler{
		skills.HookOnAfterToolResult: func(event skills.HookEvent) (skills.HookResult, error) {
			gotInput = event.Data[skills.HookDataInput]
			return skills.HookResult{}, nil
		},
	}
	rt := makeTestRuntime(t, "input-hook", toolManifest("input-hook"), nil, hooks)

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(stubFileTool{}))

	cfg := config.DefaultConfig()
	a := New(&mockProvider{}, reg, autoApprove, cfg, WithSkillRuntime(rt))

	input := `{"operation":"write","path":"main.go"}`
	ch := make(chan TurnEvent, 64)
	res := a.executeSingleTool(context.Background(), ch, provider.ToolUseBlock{
		ID: "tc-i", Name: "file", Input: json.RawMessage(input),
	})
	require.False(t, res.isError)

	inputStr, ok := gotInput.(string)
	require.True(t, ok, "HookDataInput must be a string (consumers assert .(string)); got %T", gotInput)
	assert.JSONEq(t, input, inputStr)
}

// TestAliasFileCallsCaptureCheckpoints pins alias canonicalization in the
// pipeline: models sometimes call registered aliases (write_file,
// file_write, ...) instead of the canonical file tool. The base executor
// resolves the alias and performs the write, but name-matching middlewares
// (checkpoint capture, verdict, classification) see the alias unless the
// name is canonicalized up front — leaving alias edits without an undo
// snapshot.
func TestAliasFileCallsCaptureCheckpoints(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "aliased.go")
	require.NoError(t, os.WriteFile(target, []byte("package x\n"), 0o644))

	mgr, err := checkpoint.New(dir, "sess-alias", 0)
	require.NoError(t, err)

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(stubFileTool{}))
	tools.RegisterDefaultAliases(reg)

	cfg := config.DefaultConfig()
	a := New(&mockProvider{}, reg, autoApprove, cfg, WithCheckpointManager(mgr))

	input, _ := json.Marshal(map[string]string{"operation": "write", "path": target})
	result := a.pipeline.Execute(context.Background(), toolexec.ToolCall{
		ID: "tc-a", Name: "write_file", Input: input,
	})
	require.False(t, result.IsError, "alias execution should succeed: %s", result.Content)
	require.NotEmpty(t, mgr.List(), "alias file write must capture a checkpoint for undo")
}

// oversizedNamedTool is a stub tool with an arbitrary name returning a
// fixed oversized payload.
type oversizedNamedTool struct {
	name    string
	payload string
}

func (t oversizedNamedTool) Name() string        { return t.name }
func (t oversizedNamedTool) Description() string { return "stub " + t.name }
func (t oversizedNamedTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (t oversizedNamedTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{Content: t.payload}, nil
}

// TestReadResultPagesAreNeverReOffloaded pins the offload exemption for
// read_result: its output IS retrieved offloaded content, so re-offloading
// a page above the threshold (low configured threshold, or a large limit)
// would hand the model another stub instead of the content it explicitly
// asked for — nested refs it can never resolve.
func TestReadResultPagesAreNeverReOffloaded(t *testing.T) {
	st, err := store.NewStore(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	payload := strings.Repeat("retrieved content ", 20) // ~360 bytes, over the 64-byte threshold
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(oversizedNamedTool{name: "read_result", payload: payload}))
	require.NoError(t, reg.Register(oversizedNamedTool{name: "shell", payload: payload}))

	cfg := config.DefaultConfig()
	cfg.Agent.ResultOffloadThreshold = 64
	a := New(&mockProvider{}, reg, autoApprove, cfg, WithStore(st))
	require.NotNil(t, a.resultStore)

	// Sanity: another tool's oversized output at this threshold IS offloaded.
	shellRes := a.pipeline.Execute(context.Background(), toolexec.ToolCall{
		ID: "tc-s", Name: "shell", Input: json.RawMessage(`{}`),
	})
	require.Contains(t, shellRes.Content, "ref_id=", "oversized shell output should offload at low threshold")

	// read_result pages must come back verbatim, never as another stub.
	rrRes := a.pipeline.Execute(context.Background(), toolexec.ToolCall{
		ID: "tc-rr", Name: "read_result", Input: json.RawMessage(`{"ref_id":"whatever"}`),
	})
	assert.Equal(t, payload, rrRes.Content,
		"read_result page must not be re-offloaded into a nested reference")
}
