package agentsdk

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lifecycleTask is a BackgroundTask that records the loop callbacks it
// receives. Its log is mutex-guarded because EndSession runs on its own
// goroutine, off the loop's critical path.
type lifecycleTask struct {
	mu   sync.Mutex
	log  []string
	info []BackgroundTurnInfo
}

func (t *lifecycleTask) record(event string) {
	t.mu.Lock()
	t.log = append(t.log, event)
	t.mu.Unlock()
}

func (t *lifecycleTask) StartTurn(_ context.Context, info BackgroundTurnInfo) func(context.Context) {
	t.mu.Lock()
	t.log = append(t.log, "start")
	t.info = append(t.info, info)
	t.mu.Unlock()
	return func(context.Context) { t.record("join") }
}

func (t *lifecycleTask) EndSession(context.Context) { t.record("end") }

func (t *lifecycleTask) events() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.log...)
}

func (t *lifecycleTask) turnInfos() []BackgroundTurnInfo {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]BackgroundTurnInfo(nil), t.info...)
}

// waitForEvents blocks until the task's recorded events match want, so
// assertions do not race the async EndSession goroutine.
func waitForEvents(t *testing.T, task *lifecycleTask, want []string) {
	t.Helper()
	require.Eventually(t, func() bool {
		got := task.events()
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}, 2*time.Second, 5*time.Millisecond, "events never reached %v (last: %v)", want, task.events())
}

func TestAgentBackgroundTaskObservesTurnLifecycle(t *testing.T) {
	// Turn 1 calls a tool (so the join fires); turn 2 replies with text and
	// ends the loop (no tool calls, so no join — see BackgroundTask docs).
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "echo", `{}`),
		textResponse("done"),
	}}
	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	task := &lifecycleTask{}
	a := NewAgent(p, WithTools(r), WithBackgroundTasks(task))

	ch, err := a.Turn(context.Background(), "greet the team")
	require.NoError(t, err)
	for range ch {
	}

	waitForEvents(t, task, []string{"start", "join", "start", "end"})
}

func TestAgentBackgroundTaskReceivesTurnInfo(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{textResponse("ok")}}
	task := &lifecycleTask{}

	a := NewAgent(p, WithBackgroundTasks(task))
	ch, err := a.Turn(context.Background(), "ship it")
	require.NoError(t, err)
	for range ch {
	}

	infos := task.turnInfos()
	require.Len(t, infos, 1)
	assert.Equal(t, "ship it", infos[0].UserMessage, "the turn's user message must reach background tasks")
	assert.Equal(t, 100000, infos[0].MemoryBudget, "MemoryBudget must carry the config context budget")
}

func TestAgentBackgroundTaskNoJoinWithoutToolCalls(t *testing.T) {
	// A text-only turn ends the loop without executing tools, so per the
	// BackgroundTask contract the join is never invoked.
	p := &mockProvider{responses: [][]StreamEvent{textResponse("ok")}}
	task := &lifecycleTask{}

	a := NewAgent(p, WithBackgroundTasks(task))
	ch, err := a.Turn(context.Background(), "hi")
	require.NoError(t, err)
	for range ch {
	}

	waitForEvents(t, task, []string{"start", "end"})
}

func TestAgentBackgroundTaskJoinsOnCancelledToolBatch(t *testing.T) {
	// Two tools in one response; the first cancels the context so the batch
	// aborts. The join must still run — the turn executed tools, and its
	// work has to be collected before the session-end signal.
	twoTools := []StreamEvent{
		{Type: "tool_use", ToolUse: &ToolUseBlock{ID: "tc_1", Name: "cancel_tool"}},
		{Type: "text_delta", Text: `{}`},
		{Type: "tool_use", ToolUse: &ToolUseBlock{ID: "tc_2", Name: "echo"}},
		{Type: "text_delta", Text: `{}`},
		{Type: "stop", InputTokens: 10, OutputTokens: 5},
	}
	p := &mockProvider{responses: [][]StreamEvent{twoTools}}

	r := NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, r.Register(&contextCancelTool{cancel: cancel}))
	require.NoError(t, r.Register(&echoTool{}))

	task := &lifecycleTask{}
	a := NewAgent(p, WithTools(r), WithBackgroundTasks(task))

	ch, err := a.Turn(ctx, "go")
	require.NoError(t, err)
	for range ch {
	}

	waitForEvents(t, task, []string{"start", "join", "end"})
}

func TestAgentBackgroundTaskNilIgnored(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{textResponse("ok")}}
	task := &lifecycleTask{}

	// A nil task interleaved with a real one must not panic.
	a := NewAgent(p, WithBackgroundTasks(nil, task))
	ch, err := a.Turn(context.Background(), "go")
	require.NoError(t, err)
	for range ch {
	}

	waitForEvents(t, task, []string{"start", "end"})
}

// syncLogger is a mutex-guarded Logger: EndSession panics are logged from
// their own goroutine, so an unguarded capture would race.
type syncLogger struct {
	mu    sync.Mutex
	warns []string
}

func (l *syncLogger) Warn(msg string, args ...any) {
	l.mu.Lock()
	l.warns = append(l.warns, fmt.Sprintf(msg, args...))
	l.mu.Unlock()
}

func (l *syncLogger) Error(string, ...any) {}

func (l *syncLogger) warnings() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.warns...)
}

// panicTask panics at the lifecycle stage named by its field.
type panicTask struct{ onStart, onJoin, onEnd bool }

func (p panicTask) StartTurn(context.Context, BackgroundTurnInfo) func(context.Context) {
	if p.onStart {
		panic("start boom")
	}
	return func(context.Context) {
		if p.onJoin {
			panic("join boom")
		}
	}
}

func (p panicTask) EndSession(context.Context) {
	if p.onEnd {
		panic("end boom")
	}
}

func TestAgentBackgroundTaskStartPanicRecovered(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "echo", `{}`),
		textResponse("done"),
	}}
	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	logger := &syncLogger{}
	task := &lifecycleTask{}
	a := NewAgent(p, WithTools(r), WithLogger(logger),
		WithBackgroundTasks(panicTask{onStart: true}, task))

	ch, err := a.Turn(context.Background(), "go")
	require.NoError(t, err)

	var sawDone bool
	for ev := range ch {
		if ev.Type == "done" {
			sawDone = true
		}
	}

	assert.True(t, sawDone, "a panicking StartTurn must not abort the turn")
	// The sibling task still ran its full lifecycle.
	waitForEvents(t, task, []string{"start", "join", "start", "end"})
	require.NotEmpty(t, logger.warnings())
	assert.Contains(t, logger.warnings()[0], "StartTurn panicked")
}

func TestAgentBackgroundTaskJoinPanicRecovered(t *testing.T) {
	p := &mockProvider{responses: [][]StreamEvent{
		toolCallResponse("tc_1", "echo", `{}`),
		textResponse("done"),
	}}
	r := NewRegistry()
	require.NoError(t, r.Register(&echoTool{}))

	logger := &syncLogger{}
	task := &lifecycleTask{}
	a := NewAgent(p, WithTools(r), WithLogger(logger),
		WithBackgroundTasks(panicTask{onJoin: true}, task))

	ch, err := a.Turn(context.Background(), "go")
	require.NoError(t, err)

	var sawDone bool
	for ev := range ch {
		if ev.Type == "done" {
			sawDone = true
		}
	}

	assert.True(t, sawDone, "a panicking join must not abort the turn")
	waitForEvents(t, task, []string{"start", "join", "start", "end"})
	require.NotEmpty(t, logger.warnings())
	assert.Contains(t, logger.warnings()[0], "join panicked")
}

func TestAgentBackgroundTaskEndSessionPanicRecovered(t *testing.T) {
	// EndSession runs on an unsupervised goroutine — an unrecovered panic
	// there would take down the whole process, not just the turn.
	p := &mockProvider{responses: [][]StreamEvent{textResponse("ok")}}
	logger := &syncLogger{}
	task := &lifecycleTask{}

	a := NewAgent(p, WithLogger(logger), WithBackgroundTasks(panicTask{onEnd: true}, task))
	ch, err := a.Turn(context.Background(), "go")
	require.NoError(t, err)
	for range ch {
	}

	waitForEvents(t, task, []string{"start", "end"})
	require.Eventually(t, func() bool {
		for _, w := range logger.warnings() {
			if strings.Contains(w, "EndSession panicked") {
				return true
			}
		}
		return false
	}, 2*time.Second, 5*time.Millisecond, "EndSession panic must be recovered and logged")
}
