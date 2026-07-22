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
	base := New(&mockProvider{}, tools.NewRegistry(), autoApprove, cfg)
	a := New(&mockProvider{}, reg, autoApprove, cfg, WithBackgroundTasks(nil, task, nil))

	require.Len(t, a.backgroundTasks, len(base.backgroundTasks)+1,
		"exactly one non-nil task must register; nils are filtered out")
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

// startPanickingTask panics in StartTurn; joinPanickingTask panics in the
// join it returns.
type startPanickingTask struct{}

func (startPanickingTask) StartTurn(context.Context, agentsdk.BackgroundTurnInfo) func(context.Context) {
	panic("start exploded")
}
func (startPanickingTask) EndSession(context.Context) {}

type joinPanickingTask struct{}

func (joinPanickingTask) StartTurn(context.Context, agentsdk.BackgroundTurnInfo) func(context.Context) {
	return func(context.Context) { panic("join exploded") }
}
func (joinPanickingTask) EndSession(context.Context) {}

// TestStartTurnAndJoinPanicsAreContained pins the per-task recover
// boundary on the turn-side callbacks: StartTurn and joins run on the
// main turn goroutine, so without recovery a panicking third-party task
// would abort the entire user turn as ExitPanic and starve sibling
// tasks. A healthy task must run its full lifecycle alongside a task
// panicking in StartTurn and one panicking in its join, and the turn
// must finish normally.
func TestStartTurnAndJoinPanicsAreContained(t *testing.T) {
	healthy := &recordingBackgroundTask{}
	logger := &syncWarnLogger{}

	dmp := &dynamicMockProvider{responses: [][]provider.StreamEvent{
		{
			{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "pc-1", Name: "rec_tool"}},
			{Type: "text_delta", Text: `{}`},
			{Type: "stop"},
		},
		{
			{Type: "text_delta", Text: "done"},
			{Type: "stop"},
		},
	}}

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(recordingTool{onExecute: func() {}}))

	cfg := config.DefaultConfig()
	a := New(dmp, reg, autoApprove, cfg,
		WithBackgroundTasks(startPanickingTask{}, joinPanickingTask{}, healthy),
		WithLogger(logger),
	)

	ch, err := a.Turn(context.Background(), "keep going")
	require.NoError(t, err)
	var exitReason agentsdk.TurnExitReason
	for ev := range ch {
		if ev.Type == "done" {
			exitReason = ev.ExitReason
		}
	}

	assert.NotEqual(t, agentsdk.ExitPanic, exitReason,
		"a panicking background task must not abort the user turn")
	calls := healthy.snapshot()
	assert.Contains(t, calls, "start", "healthy sibling task must still start")
	assert.Contains(t, calls, "join", "healthy sibling task must still join")
	joined := logger.joined()
	assert.Contains(t, joined, "StartTurn panicked")
	assert.Contains(t, joined, "join panicked")
}

// sessionMemoryExtractionMaxTokens mirrors the MaxTokens the extraction
// model call sets (session_memory_service.go), distinguishing it from
// foreground turn calls (config default 8192). If the service changes its
// request shape, the discriminator misroutes and these tests fail loudly.
const sessionMemoryExtractionMaxTokens = 16384

// countingProvider serves scripted responses to foreground turn calls and
// answers extraction calls (identified by request identity) separately —
// the extraction goroutine races the next foreground call, so responses
// must not be assigned by shared call order. Mutex-guarded throughout.
type countingProvider struct {
	mu                  sync.Mutex
	responses           [][]provider.StreamEvent
	foreground          int
	extraction          int
	extractionCancelled bool
}

func (p *countingProvider) Stream(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	if req.MaxTokens == sessionMemoryExtractionMaxTokens {
		// Hold the extraction call open briefly (outside the lock) so a
		// caller-side cancel — which product callers issue the moment
		// "done" is observed — would be visible during the call, the way
		// a real HTTP request bound to the context would see it.
		cancelled := false
		select {
		case <-ctx.Done():
			cancelled = true
		case <-time.After(300 * time.Millisecond):
		}

		p.mu.Lock()
		p.extraction++
		p.extractionCancelled = cancelled
		p.mu.Unlock()

		return serveStreamEvents([]provider.StreamEvent{
			{Type: "text_delta", Text: "- noted"},
			{Type: "stop"},
		}), nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.foreground >= len(p.responses) {
		return nil, fmt.Errorf("countingProvider: no more foreground responses (call #%d)", p.foreground)
	}
	events := p.responses[p.foreground]
	p.foreground++
	return serveStreamEvents(events), nil
}

func serveStreamEvents(events []provider.StreamEvent) <-chan provider.StreamEvent {
	ch := make(chan provider.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return ch
}

func (p *countingProvider) extractionCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.extraction
}

func (p *countingProvider) extractionWasCancelled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.extractionCancelled
}

// sessionMemoryFixture returns an agent whose session-memory service is
// primed to extract after a single tool round, plus the provider whose
// call count reveals the async extraction model call.
func sessionMemoryFixture(t *testing.T, turnResponses [][]provider.StreamEvent) (*Agent, *countingProvider) {
	t.Helper()

	p := &countingProvider{responses: turnResponses}

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(recordingTool{onExecute: func() {}}))

	svc := NewSessionMemoryService(t.TempDir())
	svc.SetConfig(SessionMemoryConfig{Enabled: true, ToolCallsBetweenUpdates: 1})

	cfg := config.DefaultConfig()
	a := New(p, reg, autoApprove, cfg, WithSessionMemory(svc))
	return a, p
}

// TestSessionMemoryExtractsAfterToolTurn pins the extraction trigger on
// the ordinary continue path: after a tool round the gate opens and the
// async extraction model call reaches the provider. Written green before
// the block moved onto the BackgroundTask seam; must stay green after.
func TestSessionMemoryExtractsAfterToolTurn(t *testing.T) {
	a, p := sessionMemoryFixture(t, [][]provider.StreamEvent{
		{
			{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "sm-1", Name: "rec_tool"}},
			{Type: "text_delta", Text: `{}`},
			{Type: "stop"},
		},
		{
			{Type: "text_delta", Text: "done"},
			{Type: "stop"},
		},
	})

	ch, err := a.Turn(context.Background(), "do the work")
	require.NoError(t, err)
	for range ch {
	}

	require.Eventually(t, func() bool {
		return p.extractionCalls() == 1
	}, 3*time.Second, 20*time.Millisecond,
		"exactly one extraction model call must follow the tool round")
}

// TestSessionMemoryExtractsOnTerminalToolTurn pins the seam fix: a
// task_complete turn executes tools and exits the loop immediately, but
// its tool round must still count toward session-memory extraction —
// previously the inline block sat only on the continue path, so memory
// from a session's final round was silently lost.
func TestSessionMemoryExtractsOnTerminalToolTurn(t *testing.T) {
	a, p := sessionMemoryFixture(t, [][]provider.StreamEvent{
		{
			{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "sm-2", Name: "task_complete"}},
			{Type: "text_delta", Text: `{"summary":"all done"}`},
			{Type: "stop"},
		},
	})
	require.NoError(t, a.tools.Register(tools.NewCompletionSignalTool()))

	ch, err := a.Turn(context.Background(), "finish and remember")
	require.NoError(t, err)
	for range ch {
	}

	require.Eventually(t, func() bool {
		return p.extractionCalls() == 1
	}, 3*time.Second, 20*time.Millisecond,
		"extraction must trigger after a terminal tool turn")
}

// TestExtractionDetachedFromTurnContext pins extraction's independence
// from the turn context: product callers (TUI, interactive, WS) cancel
// the turn context the moment "done" is observed, and provider HTTP
// requests are bound to their context — an extraction inheriting the
// turn context would be killed mid-flight on exactly the terminal turns
// it exists to cover. The extraction call must run on a detached
// (bounded) context that survives the caller's cancel.
func TestExtractionDetachedFromTurnContext(t *testing.T) {
	a, p := sessionMemoryFixture(t, [][]provider.StreamEvent{
		{
			{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "sm-3", Name: "task_complete"}},
			{Type: "text_delta", Text: `{"summary":"all done"}`},
			{Type: "stop"},
		},
	})
	require.NoError(t, a.tools.Register(tools.NewCompletionSignalTool()))

	turnCtx, cancel := context.WithCancel(context.Background())
	ch, err := a.Turn(turnCtx, "finish and remember")
	require.NoError(t, err)
	for range ch {
	}
	cancel() // what product callers do as soon as done is observed

	require.Eventually(t, func() bool {
		return p.extractionCalls() == 1
	}, 3*time.Second, 20*time.Millisecond, "extraction must still run")

	assert.False(t, p.extractionWasCancelled(),
		"extraction context must survive turn-context cancellation")
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
