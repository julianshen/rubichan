package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/agentsdk"
	kg "github.com/julianshen/rubichan/pkg/knowledgegraph"
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

// prefetchRecordingSelector records Select queries and RecordUsage calls.
// Select runs on the prefetch goroutine, so access is mutex-guarded.
type prefetchRecordingSelector struct {
	mu       sync.Mutex
	results  []kg.ScoredEntity
	selects  []string
	recorded []kg.ScoredEntity
	onRecord func()
}

func (s *prefetchRecordingSelector) Select(_ context.Context, query string, _ int) ([]kg.ScoredEntity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.selects = append(s.selects, query)
	return s.results, nil
}

func (s *prefetchRecordingSelector) RecordUsage(_ context.Context, entities []kg.ScoredEntity) error {
	s.mu.Lock()
	s.recorded = append(s.recorded, entities...)
	cb := s.onRecord
	s.mu.Unlock()
	if cb != nil {
		cb()
	}
	return nil
}

// TestPrefetchLoopStartsAndRecordsUsage pins the WithPrefetchManager loop
// behavior: the memory prefetch starts with the user's message as query,
// and after tool execution its entities are recorded against the agent's
// knowledge selector. Two distinct selectors separate the prefetch path
// (selA, feeds the manager) from the knowledge path (selB, receives
// RecordUsage) so the cross-wiring is unambiguous; selB returns no
// entities so the prompt builder's own Select/RecordUsage stays silent.
func TestPrefetchLoopStartsAndRecordsUsage(t *testing.T) {
	var orderMu sync.Mutex
	var order []string
	appendEvent := func(e string) {
		orderMu.Lock()
		defer orderMu.Unlock()
		order = append(order, e)
	}

	selA := &prefetchRecordingSelector{results: []kg.ScoredEntity{
		{Entity: &kg.Entity{ID: "prefetched-entity"}, Score: 1},
	}}
	selB := &prefetchRecordingSelector{onRecord: func() { appendEvent("record") }}

	dmp := &dynamicMockProvider{responses: [][]provider.StreamEvent{
		{
			{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "pf-1", Name: "rec_tool"}},
			{Type: "text_delta", Text: `{}`},
			{Type: "stop"},
		},
		{
			{Type: "text_delta", Text: "done"},
			{Type: "stop"},
		},
	}}

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(recordingTool{onExecute: func() { appendEvent("tool") }}))

	cfg := config.DefaultConfig()
	a := New(dmp, reg, autoApprove, cfg,
		WithPrefetchManager(NewPrefetchManager(selA, nil)),
		WithKnowledgeGraph(selB),
	)

	ch, err := a.Turn(context.Background(), "look this up")
	require.NoError(t, err)
	for range ch {
	}

	selA.mu.Lock()
	defer selA.mu.Unlock()
	require.NotEmpty(t, selA.selects, "memory prefetch must query the selector")
	assert.Equal(t, "look this up", selA.selects[0])

	selB.mu.Lock()
	require.Len(t, selB.recorded, 1, "prefetched entities must be recorded after tool execution")
	assert.Equal(t, "prefetched-entity", selB.recorded[0].Entity.ID)
	selB.mu.Unlock()

	orderMu.Lock()
	defer orderMu.Unlock()
	assert.Equal(t, []string{"tool", "record"}, order,
		"usage recording (join) must happen after tool execution")
}

// TestAutoDreamTriggersOnNormalLoopExit pins the auto-dream trigger to
// the BackgroundTask seam's session-end signal, which fires on every
// loop exit. Previously the trigger sat only on the max-turns exit path,
// so a session that ended normally — the overwhelmingly common case —
// could never consolidate memory regardless of the configured gate.
func TestAutoDreamTriggersOnNormalLoopExit(t *testing.T) {
	workDir := t.TempDir()
	memoryDir := filepath.Join(workDir, "memories")

	// Satisfy the consolidation gate: no lock file (last consolidation at
	// zero time) and one recent session from another session ID.
	transcriptDir := filepath.Join(workDir, ".claude", "transcripts")
	require.NoError(t, os.MkdirAll(transcriptDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(transcriptDir, "other-session.jsonl"), []byte("{}"), 0o644))

	dmp := &dynamicMockProvider{responses: [][]provider.StreamEvent{
		{
			{Type: "text_delta", Text: "all done"},
			{Type: "stop"},
		},
		{
			// Served to the async consolidation call after the loop exits.
			{Type: "text_delta", Text: "consolidated"},
			{Type: "stop"},
		},
	}}

	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	svc := NewAutoDreamService(memoryDir, AutoDreamConfig{MinHours: 1, MinSessions: 1})
	a := New(dmp, reg, autoApprove, cfg,
		WithWorkingDir(workDir),
		WithAutoDream(svc),
	)

	ch, err := a.Turn(context.Background(), "wrap up")
	require.NoError(t, err)
	for range ch {
	}

	lockPath := filepath.Join(memoryDir, ".consolidate-lock")
	require.Eventually(t, func() bool {
		_, statErr := os.Stat(lockPath)
		return statErr == nil
	}, 3*time.Second, 20*time.Millisecond,
		"consolidation must run after a normal loop exit when the gate is satisfied")
}

// TestBackgroundJoinsRunOnTerminalToolTurns pins the seam contract on
// terminal tool paths: task_complete executes its tool batch and exits the
// loop immediately, but the turn *did* execute tools, so registered
// background tasks must still be joined — before EndSession — rather than
// leaving their per-turn work uncollected.
func TestBackgroundJoinsRunOnTerminalToolTurns(t *testing.T) {
	task := &recordingBackgroundTask{}

	dmp := &dynamicMockProvider{responses: [][]provider.StreamEvent{
		{
			{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "done-1", Name: "task_complete"}},
			{Type: "text_delta", Text: `{"summary":"finished"}`},
			{Type: "stop"},
		},
	}}

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(tools.NewCompletionSignalTool()))

	cfg := config.DefaultConfig()
	a := New(dmp, reg, autoApprove, cfg, WithBackgroundTasks(task))

	ch, err := a.Turn(context.Background(), "finish up")
	require.NoError(t, err)
	for range ch {
	}

	require.Eventually(t, func() bool {
		calls := task.snapshot()
		return len(calls) > 0 && calls[len(calls)-1] == "end"
	}, 2*time.Second, 10*time.Millisecond, "EndSession must fire after the loop exits")

	assert.Equal(t, []string{"start", "join", "end"}, task.snapshot(),
		"a terminal tool turn must join background tasks before session end")
}

// TestWithBackgroundTasksFiltersNil pins the nil-guard on the public
// variadic option: a nil task must not be registered (it would panic on
// the first StartTurn dispatch), matching WithPrefetchManager/WithAutoDream.
func TestWithBackgroundTasksFiltersNil(t *testing.T) {
	task := &recordingBackgroundTask{}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	a := New(&mockProvider{}, reg, autoApprove, cfg, WithBackgroundTasks(nil, task, nil))

	require.Len(t, a.backgroundTasks, 1, "nil tasks must be filtered out at registration")
	require.NotPanics(t, func() {
		joins := a.startBackgroundTurn(context.Background(), agentsdk.BackgroundTurnInfo{})
		for _, join := range joins {
			join(context.Background())
		}
	})
}

// panickingBackgroundTask panics in EndSession.
type panickingBackgroundTask struct{}

func (panickingBackgroundTask) StartTurn(context.Context, agentsdk.BackgroundTurnInfo) func(context.Context) {
	return nil
}
func (panickingBackgroundTask) EndSession(context.Context) { panic("task exploded") }

// syncWarnLogger captures Warn calls behind a mutex — EndSession dispatch
// runs on its own goroutine, so the capture must be race-safe.
type syncWarnLogger struct {
	mu    sync.Mutex
	warns []string
}

func (l *syncWarnLogger) Debug(string, ...any) {}
func (l *syncWarnLogger) Info(string, ...any)  {}
func (l *syncWarnLogger) Warn(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warns = append(l.warns, fmt.Sprintf(format, args...))
}
func (l *syncWarnLogger) Error(string, ...any) {}

func (l *syncWarnLogger) joined() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.warns, "\n")
}

// TestEndSessionPanicIsRecovered pins the recover boundary on the
// session-end fan-out: WithBackgroundTasks is a public seam, and its
// EndSession dispatches run on dedicated goroutines — without recovery a
// buggy third-party task would take down the entire process. The panic
// must be contained and surfaced as an operator warning.
func TestEndSessionPanicIsRecovered(t *testing.T) {
	logger := &syncWarnLogger{}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	a := New(&mockProvider{}, reg, autoApprove, cfg,
		WithBackgroundTasks(panickingBackgroundTask{}),
		WithLogger(logger),
	)

	a.endBackgroundSession()

	require.Eventually(t, func() bool {
		return strings.Contains(logger.joined(), "panicked")
	}, 2*time.Second, 10*time.Millisecond,
		"a panicking EndSession must be recovered and logged, not crash the process")
}

// deadlineCapturingProvider records whether the consolidation call arrived
// with a deadline-bearing context.
type deadlineCapturingProvider struct {
	mu          sync.Mutex
	called      bool
	hadDeadline bool
}

func (p *deadlineCapturingProvider) Stream(ctx context.Context, _ provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	p.mu.Lock()
	p.called = true
	_, p.hadDeadline = ctx.Deadline()
	p.mu.Unlock()

	ch := make(chan provider.StreamEvent, 2)
	ch <- provider.StreamEvent{Type: "text_delta", Text: "consolidated"}
	ch <- provider.StreamEvent{Type: "stop"}
	close(ch)
	return ch, nil
}

// TestAutoDreamEndSessionBoundsProviderCall pins the timeout on the
// consolidation model call: EndSession runs on context.Background() by
// design (session-end work outlives the turn), so without a local
// deadline a hung provider stream would leak the goroutine forever.
func TestAutoDreamEndSessionBoundsProviderCall(t *testing.T) {
	workDir := t.TempDir()
	memoryDir := filepath.Join(workDir, "memories")
	transcriptDir := filepath.Join(workDir, ".claude", "transcripts")
	require.NoError(t, os.MkdirAll(transcriptDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(transcriptDir, "other.jsonl"), []byte("{}"), 0o644))

	p := &deadlineCapturingProvider{}
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	svc := NewAutoDreamService(memoryDir, AutoDreamConfig{MinHours: 1, MinSessions: 1})
	a := New(p, reg, autoApprove, cfg, WithWorkingDir(workDir), WithAutoDream(svc))

	task := &autoDreamBackgroundTask{agent: a, svc: svc}
	task.EndSession(context.Background())

	p.mu.Lock()
	defer p.mu.Unlock()
	require.True(t, p.called, "consolidation must reach the provider")
	assert.True(t, p.hadDeadline, "the consolidation model call must carry a deadline")
}
