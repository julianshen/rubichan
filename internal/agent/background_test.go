package agent

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// recordingBackgroundTask records the lifecycle calls the loop makes on a
// registered background task.
type recordingBackgroundTask struct {
	mu    sync.Mutex
	calls []string
	infos []agentsdk.BackgroundTurnInfo
}

func (r *recordingBackgroundTask) record(call string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call)
}

func (r *recordingBackgroundTask) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

func (r *recordingBackgroundTask) StartTurn(_ context.Context, info agentsdk.BackgroundTurnInfo) func(context.Context) {
	r.mu.Lock()
	r.infos = append(r.infos, info)
	r.mu.Unlock()
	r.record("start")
	return func(context.Context) { r.record("join") }
}

func (r *recordingBackgroundTask) EndSession(context.Context) { r.record("end") }

// recordingTool marks tool execution in the shared call log so tests can
// assert lifecycle ordering relative to tool dispatch.
type recordingTool struct{ onExecute func() }

func (recordingTool) Name() string                 { return "rec_tool" }
func (recordingTool) Description() string          { return "records execution" }
func (recordingTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t recordingTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	t.onExecute()
	return tools.ToolResult{Content: "ok"}, nil
}

// TestBackgroundTasksObserveLoopLifecycle pins the BackgroundCoordinator
// seam: a task registered via WithBackgroundTasks is started before each
// model call, joined after tool execution, and told once — asynchronously —
// when the loop ends. Turn 2 is the final text-only response; its join is
// not invoked because the join site sits on the tool-execution path
// (matching prefetch semantics, where unjoined async work still completes).
func TestBackgroundTasksObserveLoopLifecycle(t *testing.T) {
	task := &recordingBackgroundTask{}

	dmp := &dynamicMockProvider{responses: [][]provider.StreamEvent{
		{
			{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "bg-1", Name: "rec_tool"}},
			{Type: "text_delta", Text: `{}`},
			{Type: "stop"},
		},
		{
			{Type: "text_delta", Text: "done"},
			{Type: "stop"},
		},
	}}

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(recordingTool{onExecute: func() { task.record("tool") }}))

	cfg := config.DefaultConfig()
	a := New(dmp, reg, autoApprove, cfg, WithBackgroundTasks(task))

	ch, err := a.Turn(context.Background(), "do the thing")
	require.NoError(t, err)
	for range ch {
	}

	require.Eventually(t, func() bool {
		calls := task.snapshot()
		return len(calls) > 0 && calls[len(calls)-1] == "end"
	}, 2*time.Second, 10*time.Millisecond, "EndSession must fire after the loop exits")

	assert.Equal(t, []string{"start", "tool", "join", "start", "end"}, task.snapshot())

	task.mu.Lock()
	defer task.mu.Unlock()
	require.Len(t, task.infos, 2)
	assert.Equal(t, "do the thing", task.infos[0].UserMessage)
	assert.Equal(t, "do the thing", task.infos[1].UserMessage)
}
