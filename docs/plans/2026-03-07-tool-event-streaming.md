# Tool Event Streaming Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add channel-based streaming tool execution so the TUI shows real-time output during long-running tool operations.

**Architecture:** Optional `StreamingTool` interface with Begin/Delta/End events. `Pipeline.ExecuteStream` returns a `<-chan StreamEvent` that emits progress events followed by a final result. The emitter function is transported through the middleware chain via `context.WithValue` so existing middlewares remain unchanged. The `RegistryExecutor` detects `StreamingTool` and calls `ExecuteStream` with the emitter extracted from context.

**Tech Stack:** Go, `context.WithValue` for emitter transport, `chan StreamEvent` for pipeline streaming, `os/exec` pipes for shell/search tool streaming.

---

### Task 1: ToolEvent and StreamingTool Interface

**Files:**
- Modify: `internal/tools/interface.go`
- Modify: `internal/tools/interface_test.go`

**Step 1: Write the failing tests**

Add to `internal/tools/interface_test.go`:

```go
func TestEventStageConstants(t *testing.T) {
	assert.Equal(t, EventStage(0), EventBegin)
	assert.Equal(t, EventStage(1), EventDelta)
	assert.Equal(t, EventStage(2), EventEnd)
}

func TestToolEventFields(t *testing.T) {
	ev := ToolEvent{Stage: EventDelta, Content: "line 1", IsError: false}
	assert.Equal(t, EventDelta, ev.Stage)
	assert.Equal(t, "line 1", ev.Content)
	assert.False(t, ev.IsError)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/tools/ -run TestEventStage -v && go test ./internal/tools/ -run TestToolEventFields -v`
Expected: FAIL — `EventStage`, `EventBegin`, etc. undefined

**Step 3: Write minimal implementation**

Add to `internal/tools/interface.go`:

```go
// EventStage represents the lifecycle stage of a streaming tool event.
type EventStage int

const (
	// EventBegin signals the start of a streaming tool execution.
	EventBegin EventStage = iota
	// EventDelta carries incremental output during execution.
	EventDelta
	// EventEnd signals the completion of streaming execution.
	EventEnd
)

// ToolEvent represents a streaming event emitted during tool execution.
type ToolEvent struct {
	Stage   EventStage
	Content string
	IsError bool
}

// StreamingTool extends Tool with streaming execution capability.
// Tools that implement this interface emit real-time progress events
// during execution. Tools that don't implement it fall back to
// synchronous Execute().
type StreamingTool interface {
	Tool
	ExecuteStream(ctx context.Context, input json.RawMessage, emit func(ToolEvent)) (ToolResult, error)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/tools/ -run "TestEventStage|TestToolEventFields" -v`
Expected: PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add ToolEvent, EventStage, and StreamingTool interface
```

---

### Task 2: Context-Based Emitter Transport

**Files:**
- Modify: `internal/tools/interface.go`
- Modify: `internal/tools/interface_test.go`

**Step 1: Write the failing tests**

Add to `internal/tools/interface_test.go`:

```go
func TestWithEmitterRoundTrip(t *testing.T) {
	var received []ToolEvent
	emit := func(ev ToolEvent) {
		received = append(received, ev)
	}

	ctx := WithEmitter(context.Background(), emit)
	got := EmitterFromContext(ctx)
	require.NotNil(t, got)

	got(ToolEvent{Stage: EventBegin, Content: "start"})
	got(ToolEvent{Stage: EventDelta, Content: "data"})

	assert.Len(t, received, 2)
	assert.Equal(t, EventBegin, received[0].Stage)
	assert.Equal(t, "data", received[1].Content)
}

func TestEmitterFromContextReturnsNilWhenNotSet(t *testing.T) {
	emit := EmitterFromContext(context.Background())
	assert.Nil(t, emit)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/tools/ -run "TestWithEmitter|TestEmitterFromContext" -v`
Expected: FAIL — `WithEmitter`, `EmitterFromContext` undefined

**Step 3: Write minimal implementation**

Add to `internal/tools/interface.go`:

```go
type emitterContextKey struct{}

// WithEmitter returns a new context carrying the given emit function.
func WithEmitter(ctx context.Context, emit func(ToolEvent)) context.Context {
	return context.WithValue(ctx, emitterContextKey{}, emit)
}

// EmitterFromContext extracts the emit function from the context.
// Returns nil if no emitter was set.
func EmitterFromContext(ctx context.Context) func(ToolEvent) {
	if emit, ok := ctx.Value(emitterContextKey{}).(func(ToolEvent)); ok {
		return emit
	}
	return nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/tools/ -run "TestWithEmitter|TestEmitterFromContext" -v`
Expected: PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add context-based emitter transport for streaming tools
```

---

### Task 3: Pipeline.ExecuteStream

**Files:**
- Modify: `internal/toolexec/pipeline.go`
- Modify: `internal/toolexec/pipeline_test.go`

**Step 1: Write the failing tests**

Add to `internal/toolexec/pipeline_test.go`:

```go
func TestPipelineExecuteStreamFinalResult(t *testing.T) {
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "sync result", DisplayContent: "display"}
	}

	p := toolexec.NewPipeline(base)
	ch := p.ExecuteStream(context.Background(), toolexec.ToolCall{
		ID: "call-1", Name: "test",
	})

	var events []toolexec.StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	require.Len(t, events, 1)
	assert.Equal(t, toolexec.StreamFinal, events[0].Type)
	require.NotNil(t, events[0].Result)
	assert.Equal(t, "sync result", events[0].Result.Content)
	assert.Equal(t, "display", events[0].Result.DisplayContent)
}

func TestPipelineExecuteStreamWithProgressEvents(t *testing.T) {
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		emit := tools.EmitterFromContext(ctx)
		if emit != nil {
			emit(tools.ToolEvent{Stage: tools.EventBegin, Content: "starting"})
			emit(tools.ToolEvent{Stage: tools.EventDelta, Content: "line 1\n"})
			emit(tools.ToolEvent{Stage: tools.EventDelta, Content: "line 2\n"})
			emit(tools.ToolEvent{Stage: tools.EventEnd, Content: "done"})
		}
		return toolexec.Result{Content: "final"}
	}

	p := toolexec.NewPipeline(base)
	ch := p.ExecuteStream(context.Background(), toolexec.ToolCall{
		ID: "call-2", Name: "streaming_test",
	})

	var events []toolexec.StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// 4 progress events + 1 final
	require.Len(t, events, 5)
	assert.Equal(t, toolexec.StreamProgress, events[0].Type)
	assert.Equal(t, "starting", events[0].Event.Content)
	assert.Equal(t, toolexec.StreamProgress, events[1].Type)
	assert.Equal(t, "line 1\n", events[1].Event.Content)
	assert.Equal(t, toolexec.StreamProgress, events[2].Type)
	assert.Equal(t, toolexec.StreamFinal, events[4].Type)
	assert.Equal(t, "final", events[4].Result.Content)
}

func TestPipelineExecuteStreamMiddlewarePreservesEmitter(t *testing.T) {
	var progressReceived bool
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		emit := tools.EmitterFromContext(ctx)
		if emit != nil {
			emit(tools.ToolEvent{Stage: tools.EventDelta, Content: "progress"})
			progressReceived = true
		}
		return toolexec.Result{Content: "done"}
	}

	// Middleware that wraps but passes context through.
	wrapper := func(next toolexec.HandlerFunc) toolexec.HandlerFunc {
		return func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
			return next(ctx, tc)
		}
	}

	p := toolexec.NewPipeline(base, wrapper)
	ch := p.ExecuteStream(context.Background(), toolexec.ToolCall{
		ID: "call-3", Name: "test",
	})

	var events []toolexec.StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	assert.True(t, progressReceived)
	require.Len(t, events, 2) // 1 progress + 1 final
	assert.Equal(t, toolexec.StreamProgress, events[0].Type)
	assert.Equal(t, toolexec.StreamFinal, events[1].Type)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/toolexec/ -run "TestPipelineExecuteStream" -v`
Expected: FAIL — `ExecuteStream`, `StreamEvent`, `StreamFinal`, `StreamProgress` undefined

**Step 3: Write minimal implementation**

Add to `internal/toolexec/pipeline.go`:

```go
import "github.com/julianshen/rubichan/internal/tools"

// StreamEventType distinguishes progress events from the final result.
type StreamEventType int

const (
	// StreamProgress carries a ToolEvent during execution.
	StreamProgress StreamEventType = iota
	// StreamFinal carries the completed Result.
	StreamFinal
)

// StreamEvent is either a progress event or a final result from the pipeline.
type StreamEvent struct {
	Type   StreamEventType
	Event  *tools.ToolEvent // set when Type == StreamProgress
	Result *Result          // set when Type == StreamFinal
}

// ExecuteStream runs the pipeline in a goroutine and returns a channel
// of StreamEvents. Progress events (from StreamingTool implementations)
// arrive first, followed by a single StreamFinal event with the result.
// The channel is closed after the final event.
func (p *Pipeline) ExecuteStream(ctx context.Context, tc ToolCall) <-chan StreamEvent {
	ch := make(chan StreamEvent, 32)
	go func() {
		defer close(ch)
		emit := func(ev tools.ToolEvent) {
			ch <- StreamEvent{Type: StreamProgress, Event: &ev}
		}
		emitCtx := tools.WithEmitter(ctx, emit)
		result := p.Execute(emitCtx, tc)
		ch <- StreamEvent{Type: StreamFinal, Result: &result}
	}()
	return ch
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/toolexec/ -run "TestPipelineExecuteStream" -v`
Expected: PASS

**Step 5: Run all tests**

Run: `go test ./internal/toolexec/... -v`
Expected: All PASS (existing tests unaffected)

**Step 6: Commit**

```
[BEHAVIORAL] Add Pipeline.ExecuteStream for channel-based streaming
```

---

### Task 4: RegistryExecutor StreamingTool Detection

**Files:**
- Modify: `internal/toolexec/executor.go`
- Modify: `internal/toolexec/executor_test.go`

**Step 1: Write the failing tests**

Add to `internal/toolexec/executor_test.go`:

```go
// streamingStubTool implements both tools.Tool and tools.StreamingTool.
type streamingStubTool struct {
	stubTool
	streamResult tools.ToolResult
	streamErr    error
	emittedEvents []tools.ToolEvent
}

func (s *streamingStubTool) ExecuteStream(ctx context.Context, input json.RawMessage, emit func(tools.ToolEvent)) (tools.ToolResult, error) {
	s.execInput = input
	emit(tools.ToolEvent{Stage: tools.EventBegin, Content: "begin"})
	emit(tools.ToolEvent{Stage: tools.EventDelta, Content: "output line 1\n"})
	emit(tools.ToolEvent{Stage: tools.EventEnd, Content: "end"})
	return s.streamResult, s.streamErr
}

func TestRegistryExecutorUsesStreamingToolWhenEmitterPresent(t *testing.T) {
	stub := &streamingStubTool{
		stubTool: stubTool{
			name:   "streaming_shell",
			schema: json.RawMessage(`{}`),
		},
		streamResult: tools.ToolResult{Content: "streamed result"},
	}

	registry := tools.NewRegistry()
	require.NoError(t, registry.Register(stub))

	var received []tools.ToolEvent
	emit := func(ev tools.ToolEvent) {
		received = append(received, ev)
	}

	ctx := tools.WithEmitter(context.Background(), emit)
	handler := toolexec.RegistryExecutor(registry)
	result := handler(ctx, toolexec.ToolCall{
		ID: "call-s1", Name: "streaming_shell",
		Input: json.RawMessage(`{"command":"echo hi"}`),
	})

	assert.Equal(t, "streamed result", result.Content)
	assert.False(t, result.IsError)
	require.Len(t, received, 3)
	assert.Equal(t, tools.EventBegin, received[0].Stage)
	assert.Equal(t, tools.EventDelta, received[1].Stage)
	assert.Equal(t, tools.EventEnd, received[2].Stage)
}

func TestRegistryExecutorStreamingToolFallsBackWithoutEmitter(t *testing.T) {
	stub := &streamingStubTool{
		stubTool: stubTool{
			name:   "streaming_shell",
			schema: json.RawMessage(`{}`),
		},
		streamResult: tools.ToolResult{Content: "streamed result"},
	}

	registry := tools.NewRegistry()
	require.NoError(t, registry.Register(stub))

	// No emitter in context — should still use ExecuteStream with a no-op emitter
	handler := toolexec.RegistryExecutor(registry)
	result := handler(context.Background(), toolexec.ToolCall{
		ID: "call-s2", Name: "streaming_shell",
		Input: json.RawMessage(`{}`),
	})

	assert.Equal(t, "streamed result", result.Content)
	assert.False(t, result.IsError)
}

func TestRegistryExecutorStreamingToolError(t *testing.T) {
	stub := &streamingStubTool{
		stubTool: stubTool{
			name:   "streaming_shell",
			schema: json.RawMessage(`{}`),
		},
		streamErr: errors.New("stream failed"),
	}

	registry := tools.NewRegistry()
	require.NoError(t, registry.Register(stub))

	handler := toolexec.RegistryExecutor(registry)
	result := handler(context.Background(), toolexec.ToolCall{
		ID: "call-s3", Name: "streaming_shell",
	})

	assert.Contains(t, result.Content, "stream failed")
	assert.True(t, result.IsError)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/toolexec/ -run "TestRegistryExecutorUsesStreaming|TestRegistryExecutorStreamingTool" -v`
Expected: FAIL — tests pass but without expected behavior (streaming not detected)

**Step 3: Write minimal implementation**

Modify `internal/toolexec/executor.go` — update `RegistryExecutor`:

```go
func RegistryExecutor(lookup ToolLookup) HandlerFunc {
	return func(ctx context.Context, tc ToolCall) Result {
		tool, ok := lookup.Get(tc.Name)
		if !ok {
			return Result{
				Content: fmt.Sprintf("unknown tool: %s", tc.Name),
				IsError: true,
			}
		}

		// Prefer streaming execution when the tool supports it.
		if st, ok := tool.(tools.StreamingTool); ok {
			emit := tools.EmitterFromContext(ctx)
			if emit == nil {
				emit = func(tools.ToolEvent) {}
			}
			tr, err := st.ExecuteStream(ctx, tc.Input, emit)
			if err != nil {
				return Result{
					Content: fmt.Sprintf("tool execution error: %s", err.Error()),
					IsError: true,
				}
			}
			return Result{
				Content:        tr.Content,
				DisplayContent: tr.DisplayContent,
				IsError:        tr.IsError,
			}
		}

		tr, err := tool.Execute(ctx, tc.Input)
		if err != nil {
			return Result{
				Content: fmt.Sprintf("tool execution error: %s", err.Error()),
				IsError: true,
			}
		}

		return Result{
			Content:        tr.Content,
			DisplayContent: tr.DisplayContent,
			IsError:        tr.IsError,
		}
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/toolexec/ -v`
Expected: All PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add StreamingTool detection to RegistryExecutor
```

---

### Task 5: Agent TurnEvent for Tool Progress

**Files:**
- Modify: `internal/agent/agent.go`
- Modify: `internal/agent/agent_test.go`

**Step 1: Write the failing test**

Add to `internal/agent/agent_test.go`:

```go
func TestTurnEmitsToolProgressEvents(t *testing.T) {
	// Create a streaming mock tool.
	streamTool := &mockStreamingTool{
		mockTool: mockTool{
			name:        "stream_test",
			description: "streaming test tool",
			inputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
			executeFn: func(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
				return tools.ToolResult{Content: "sync fallback"}, nil
			},
		},
		streamFn: func(ctx context.Context, input json.RawMessage, emit func(tools.ToolEvent)) (tools.ToolResult, error) {
			emit(tools.ToolEvent{Stage: tools.EventBegin, Content: "starting"})
			emit(tools.ToolEvent{Stage: tools.EventDelta, Content: "line 1\n"})
			emit(tools.ToolEvent{Stage: tools.EventEnd, Content: "done"})
			return tools.ToolResult{Content: "streamed output"}, nil
		},
	}

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(streamTool))

	mp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "tu-1", Name: "stream_test"}},
				{Type: "text_delta", Text: "{}"},
				{Type: "stop"},
			},
			{
				{Type: "text_delta", Text: "All done"},
				{Type: "stop"},
			},
		},
	}

	cfg := config.DefaultConfig()
	a := New(mp, reg, autoApprove, cfg)

	ch, err := a.Turn(context.Background(), "test streaming")
	require.NoError(t, err)

	var progressEvents []TurnEvent
	var hasToolResult bool
	for ev := range ch {
		if ev.Type == "tool_progress" {
			progressEvents = append(progressEvents, ev)
		}
		if ev.Type == "tool_result" {
			hasToolResult = true
		}
	}

	assert.True(t, hasToolResult, "should have a tool_result event")
	assert.GreaterOrEqual(t, len(progressEvents), 1, "should have tool_progress events")
	assert.NotNil(t, progressEvents[0].ToolProgress)
	assert.Equal(t, "stream_test", progressEvents[0].ToolProgress.Name)
}
```

Also add the `mockStreamingTool` type near the top of the test file:

```go
type mockStreamingTool struct {
	mockTool
	streamFn func(ctx context.Context, input json.RawMessage, emit func(tools.ToolEvent)) (tools.ToolResult, error)
}

func (m *mockStreamingTool) ExecuteStream(ctx context.Context, input json.RawMessage, emit func(tools.ToolEvent)) (tools.ToolResult, error) {
	return m.streamFn(ctx, input, emit)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestTurnEmitsToolProgressEvents -v`
Expected: FAIL — `ToolProgressEvent`, `ToolProgress` field undefined

**Step 3: Write minimal implementation**

In `internal/agent/agent.go`:

1. Add `ToolProgressEvent` struct and `ToolProgress` field to `TurnEvent`:

```go
// ToolProgressEvent contains details about streaming tool progress.
type ToolProgressEvent struct {
	ID      string
	Name    string
	Stage   int
	Content string
	IsError bool
}
```

Add to `TurnEvent`:
```go
ToolProgress   *ToolProgressEvent // populated for tool_progress events
```

2. Change `executeSingleTool` signature to accept `ch`:

```go
func (a *Agent) executeSingleTool(ctx context.Context, ch chan<- TurnEvent, tc provider.ToolUseBlock) toolExecResult {
	stream := a.pipeline.ExecuteStream(ctx, toolexec.ToolCall{
		ID: tc.ID, Name: tc.Name, Input: tc.Input,
	})
	var finalResult toolexec.Result
	for ev := range stream {
		switch ev.Type {
		case toolexec.StreamProgress:
			if ev.Event != nil {
				ch <- TurnEvent{
					Type: "tool_progress",
					ToolProgress: &ToolProgressEvent{
						ID:      tc.ID,
						Name:    tc.Name,
						Stage:   int(ev.Event.Stage),
						Content: ev.Event.Content,
						IsError: ev.Event.IsError,
					},
				}
			}
		case toolexec.StreamFinal:
			if ev.Result != nil {
				finalResult = *ev.Result
			}
		}
	}
	return toolExecResult{
		toolUseID: tc.ID,
		content:   finalResult.Content,
		isError:   finalResult.IsError,
		event:     makeToolResultEvent(tc.ID, tc.Name, finalResult.Content, finalResult.DisplayContent, finalResult.IsError),
	}
}
```

3. Update all call sites of `executeSingleTool` to pass `ch`:

In `executeToolsSequential`:
```go
r := a.executeSingleToolWithApproval(ctx, ch, tc)
```

In `executeTools` (parallel path):
```go
p.Go(func() {
    res := a.executeSingleTool(ctx, ch, it.tc)
    // ...
})
```

And in `executeSingleToolWithApproval`:
```go
func (a *Agent) executeSingleToolWithApproval(ctx context.Context, ch chan<- TurnEvent, tc provider.ToolUseBlock) toolExecResult {
    // ... approval check unchanged ...
    return a.executeSingleTool(ctx, ch, tc)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/agent/ -run TestTurnEmitsToolProgressEvents -v`
Expected: PASS

**Step 5: Run all agent tests**

Run: `go test ./internal/agent/... -v`
Expected: All PASS

**Step 6: Commit**

```
[BEHAVIORAL] Add tool_progress TurnEvent and channel-based streaming in agent
```

---

### Task 6: TUI Rendering of tool_progress Events

**Files:**
- Modify: `internal/tui/update.go`

**Step 1: Write the failing test (if a TUI update test file exists with handleTurnEvent tests)**

If no existing test covers this, add a basic test or verify manually. The key change is adding a case in `handleTurnEvent`.

**Step 2: Implement the handler**

Add a new case in `handleTurnEvent` in `internal/tui/update.go`:

```go
case "tool_progress":
    if msg.ToolProgress != nil {
        m.content.WriteString(msg.ToolProgress.Content)
        m.setContentAndAutoScroll(m.content.String())
    }
    return m, m.waitForEvent()
```

**Step 3: Run all TUI tests**

Run: `go test ./internal/tui/... -v`
Expected: All PASS

**Step 4: Commit**

```
[BEHAVIORAL] Add tool_progress rendering in TUI update handler
```

---

### Task 7: ShellTool Streaming Implementation

**Files:**
- Modify: `internal/tools/shell.go`
- Modify: `internal/tools/shell_test.go`

**Step 1: Write the failing tests**

Add to `internal/tools/shell_test.go`:

```go
func TestShellToolImplementsStreamingTool(t *testing.T) {
	st := NewShellTool(t.TempDir(), 30*time.Second)
	var tool Tool = st
	_, ok := tool.(StreamingTool)
	assert.True(t, ok, "ShellTool should implement StreamingTool")
}

func TestShellToolExecuteStreamEmitsEvents(t *testing.T) {
	dir := t.TempDir()
	st := NewShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]string{
		"command": "echo hello && echo world",
	})

	var events []ToolEvent
	emit := func(ev ToolEvent) {
		events = append(events, ev)
	}

	result, err := st.ExecuteStream(context.Background(), input, emit)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "hello")
	assert.Contains(t, result.Content, "world")

	// Should have at least Begin and End events.
	require.GreaterOrEqual(t, len(events), 2)
	assert.Equal(t, EventBegin, events[0].Stage)
	assert.Equal(t, EventEnd, events[len(events)-1].Stage)

	// Should have Delta events with actual output.
	var deltas []ToolEvent
	for _, ev := range events {
		if ev.Stage == EventDelta {
			deltas = append(deltas, ev)
		}
	}
	assert.GreaterOrEqual(t, len(deltas), 1, "should have delta events with output lines")
}

func TestShellToolExecuteStreamTimeout(t *testing.T) {
	dir := t.TempDir()
	st := NewShellTool(dir, 200*time.Millisecond)

	input, _ := json.Marshal(map[string]string{
		"command": "echo before && sleep 10",
	})

	var events []ToolEvent
	emit := func(ev ToolEvent) {
		events = append(events, ev)
	}

	result, err := st.ExecuteStream(context.Background(), input, emit)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "timed out")
}

func TestShellToolExecuteStreamExitCode(t *testing.T) {
	dir := t.TempDir()
	st := NewShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]string{
		"command": "echo error >&2; exit 1",
	})

	var events []ToolEvent
	emit := func(ev ToolEvent) {
		events = append(events, ev)
	}

	result, err := st.ExecuteStream(context.Background(), input, emit)
	require.NoError(t, err)
	assert.True(t, result.IsError)

	// End event should indicate error.
	require.GreaterOrEqual(t, len(events), 2)
	assert.Equal(t, EventEnd, events[len(events)-1].Stage)
	assert.True(t, events[len(events)-1].IsError)
}

func TestShellToolExecuteStreamInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	st := NewShellTool(dir, 30*time.Second)

	result, err := st.ExecuteStream(context.Background(), json.RawMessage(`{invalid`), func(ev ToolEvent) {})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid")
}

func TestShellToolExecuteStreamBlockedCommand(t *testing.T) {
	dir := t.TempDir()
	st := NewShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]string{
		"command": "apply_patch foo",
	})

	var events []ToolEvent
	result, err := st.ExecuteStream(context.Background(), input, func(ev ToolEvent) {
		events = append(events, ev)
	})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "routing")
	// No events should be emitted for blocked commands.
	assert.Empty(t, events)
}

func TestShellToolExecuteStreamDetectsChanges(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir, "tracked.txt")

	dt := NewDiffTracker()
	st := NewShellTool(dir, 30*time.Second)
	st.SetDiffTracker(dt)

	input, _ := json.Marshal(map[string]string{
		"command": "echo modified > tracked.txt",
	})

	result, err := st.ExecuteStream(context.Background(), input, func(ev ToolEvent) {})
	require.NoError(t, err)
	assert.False(t, result.IsError)

	changes := dt.Changes()
	require.GreaterOrEqual(t, len(changes), 1)
	pathSet := make(map[string]bool)
	for _, c := range changes {
		pathSet[c.Path] = true
	}
	assert.True(t, pathSet["tracked.txt"])
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/tools/ -run "TestShellToolImplementsStreaming|TestShellToolExecuteStream" -v`
Expected: FAIL — `ExecuteStream` method not defined on ShellTool

**Step 3: Write the streaming implementation**

Add `ExecuteStream` method to `ShellTool` in `internal/tools/shell.go`:

```go
// ExecuteStream implements StreamingTool. It streams stdout/stderr
// line-by-line as EventDelta events during command execution.
func (s *ShellTool) ExecuteStream(ctx context.Context, input json.RawMessage, emit func(ToolEvent)) (ToolResult, error) {
	var in shellInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	interception := inspectShellCommand(in.Command, s.workDir)
	if interception.routeReason != "" {
		return ToolResult{
			Content: fmt.Sprintf("command requires routing: %s. Use the file tool for this operation.", interception.routeReason),
			IsError: true,
		}, nil
	}
	if interception.blockReason != "" {
		return ToolResult{
			Content: fmt.Sprintf("command blocked: %s. Use the file tool for file edits.", interception.blockReason),
			IsError: true,
		}, nil
	}

	var baseline map[string]bool
	if s.diffTracker != nil {
		baseline = s.captureBaseline()
	}

	emit(ToolEvent{Stage: EventBegin, Content: fmt.Sprintf("$ %s\n", in.Command)})

	timeoutCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, "sh", "-c", in.Command)
	cmd.Dir = s.workDir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to create stdout pipe: %s", err), IsError: true}, nil
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to create stderr pipe: %s", err), IsError: true}, nil
	}

	if err := cmd.Start(); err != nil {
		return ToolResult{Content: fmt.Sprintf("failed to start command: %s", err), IsError: true}, nil
	}

	// Read stdout and stderr concurrently, emitting lines as deltas.
	var output strings.Builder
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text() + "\n"
			output.WriteString(line)
			emit(ToolEvent{Stage: EventDelta, Content: line})
		}
	}()
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text() + "\n"
			output.WriteString(line)
			emit(ToolEvent{Stage: EventDelta, Content: line, IsError: true})
		}
	}()

	wg.Wait()
	cmdErr := cmd.Wait()

	if s.diffTracker != nil {
		s.detectChanges(baseline)
	}

	content := output.String()
	var displayContent string

	// Check timeout.
	if timeoutCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
		emit(ToolEvent{Stage: EventEnd, Content: fmt.Sprintf("timed out after %s", s.timeout), IsError: true})
		return withInterceptionWarnings(ToolResult{
			Content: fmt.Sprintf("command timed out after %s", s.timeout),
			IsError: true,
		}, interception.warnings), nil
	}

	// Truncate for LLM.
	if len(content) > maxOutputBytes {
		displayContent = content
		if len(displayContent) > maxDisplayBytes {
			displayContent = displayContent[:maxDisplayBytes] + "\n... output truncated"
		}
		content = content[:maxOutputBytes] + "\n... output truncated"
	}

	isError := cmdErr != nil
	emit(ToolEvent{Stage: EventEnd, Content: "", IsError: isError})

	return withInterceptionWarnings(ToolResult{
		Content: content, DisplayContent: displayContent, IsError: isError,
	}, interception.warnings), nil
}
```

Note: Add `"bufio"` and `"sync"` to the imports in `shell.go`.

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/tools/ -run "TestShellTool" -v`
Expected: All PASS (both old Execute tests and new ExecuteStream tests)

**Step 5: Commit**

```
[BEHAVIORAL] Implement StreamingTool for ShellTool with line-by-line streaming
```

---

### Task 8: SearchTool Streaming Implementation

**Files:**
- Modify: `internal/tools/search.go`
- Modify: `internal/tools/search_test.go`

**Step 1: Write the failing tests**

Add to `internal/tools/search_test.go`:

```go
func TestSearchToolImplementsStreamingTool(t *testing.T) {
	st := NewSearchTool(t.TempDir())
	var tool Tool = st
	_, ok := tool.(StreamingTool)
	assert.True(t, ok, "SearchTool should implement StreamingTool")
}

func TestSearchToolExecuteStreamEmitsEvents(t *testing.T) {
	dir := setupSearchDir(t)
	st := NewSearchTool(dir)

	input, _ := json.Marshal(searchInput{Pattern: "func"})

	var events []ToolEvent
	emit := func(ev ToolEvent) {
		events = append(events, ev)
	}

	result, err := st.ExecuteStream(context.Background(), input, emit)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "func")

	require.GreaterOrEqual(t, len(events), 2)
	assert.Equal(t, EventBegin, events[0].Stage)
	assert.Equal(t, EventEnd, events[len(events)-1].Stage)

	var deltas []ToolEvent
	for _, ev := range events {
		if ev.Stage == EventDelta {
			deltas = append(deltas, ev)
		}
	}
	assert.GreaterOrEqual(t, len(deltas), 1)
}

func TestSearchToolExecuteStreamNoMatches(t *testing.T) {
	dir := setupSearchDir(t)
	st := NewSearchTool(dir)

	input, _ := json.Marshal(searchInput{Pattern: "nonexistent_xyz"})

	var events []ToolEvent
	result, err := st.ExecuteStream(context.Background(), input, func(ev ToolEvent) {
		events = append(events, ev)
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "no matches")

	require.GreaterOrEqual(t, len(events), 2)
	assert.Equal(t, EventBegin, events[0].Stage)
	assert.Equal(t, EventEnd, events[len(events)-1].Stage)
}

func TestSearchToolExecuteStreamInvalidInput(t *testing.T) {
	st := NewSearchTool(t.TempDir())
	result, err := st.ExecuteStream(context.Background(), json.RawMessage(`{bad`), func(ev ToolEvent) {})
	require.NoError(t, err)
	assert.True(t, result.IsError)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/tools/ -run "TestSearchToolImplementsStreaming|TestSearchToolExecuteStream" -v`
Expected: FAIL — `ExecuteStream` not defined on SearchTool

**Step 3: Write the streaming implementation**

Add `ExecuteStream` method to `SearchTool` in `internal/tools/search.go`:

```go
// ExecuteStream implements StreamingTool. It emits match lines as
// EventDelta events during search execution.
func (s *SearchTool) ExecuteStream(ctx context.Context, input json.RawMessage, emit func(ToolEvent)) (ToolResult, error) {
	var in searchInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	if in.Pattern == "" {
		return ToolResult{Content: "pattern is required", IsError: true}, nil
	}

	if _, err := regexp.Compile(in.Pattern); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid regex pattern: %s", err), IsError: true}, nil
	}

	searchDir := s.rootDir
	if in.Path != "" {
		if filepath.IsAbs(in.Path) {
			return ToolResult{Content: "path traversal denied: absolute paths not allowed", IsError: true}, nil
		}
		candidate := filepath.Join(s.rootDir, in.Path)
		abs, err := filepath.Abs(candidate)
		if err != nil {
			return ToolResult{Content: fmt.Sprintf("path traversal denied: %s", err), IsError: true}, nil
		}
		if !strings.HasPrefix(abs, s.rootDir+string(filepath.Separator)) && abs != s.rootDir {
			return ToolResult{Content: "path traversal denied: path escapes root directory", IsError: true}, nil
		}
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			if os.IsNotExist(err) {
				return ToolResult{Content: fmt.Sprintf("path not found: %s", in.Path), IsError: true}, nil
			}
			return ToolResult{Content: fmt.Sprintf("path traversal denied: %s", err), IsError: true}, nil
		}
		if !strings.HasPrefix(resolved, s.rootDir+string(filepath.Separator)) && resolved != s.rootDir {
			return ToolResult{Content: "path traversal denied: path escapes root directory via symlink", IsError: true}, nil
		}
		searchDir = resolved
	}

	if in.MaxResults <= 0 {
		in.MaxResults = 200
	}
	if in.ContextLines < 0 {
		in.ContextLines = 0
	}

	emit(ToolEvent{Stage: EventBegin, Content: fmt.Sprintf("searching for %q\n", in.Pattern)})

	var result string
	var searchErr error
	if rgPath, lookErr := exec.LookPath("rg"); lookErr == nil {
		result, searchErr = s.searchWithRipgrepStreaming(ctx, rgPath, searchDir, in, emit)
	} else {
		result, searchErr = s.searchGoNative(ctx, searchDir, in)
		// Emit all Go-native results as a single delta.
		if searchErr == nil && result != "" {
			emit(ToolEvent{Stage: EventDelta, Content: result})
		}
	}

	if searchErr != nil {
		emit(ToolEvent{Stage: EventEnd, Content: searchErr.Error(), IsError: true})
		return ToolResult{Content: fmt.Sprintf("search error: %s", searchErr), IsError: true}, nil
	}

	if result == "" {
		emit(ToolEvent{Stage: EventEnd, Content: "no matches found"})
		return ToolResult{Content: "no matches found"}, nil
	}

	var displayContent string
	if len(result) > maxOutputBytes {
		display := result
		if len(display) > maxDisplayBytes {
			display = display[:maxDisplayBytes] + "\n... output truncated"
		}
		displayContent = display
		result = result[:maxOutputBytes] + "\n... output truncated"
	}

	emit(ToolEvent{Stage: EventEnd, Content: ""})
	return ToolResult{Content: result, DisplayContent: displayContent}, nil
}

func (s *SearchTool) searchWithRipgrepStreaming(ctx context.Context, rgPath, searchDir string, in searchInput, emit func(ToolEvent)) (string, error) {
	args := []string{
		"--no-heading", "--line-number", "--color", "never",
	}
	if in.ContextLines > 0 {
		args = append(args, fmt.Sprintf("-C%d", in.ContextLines))
	}
	if in.FilePattern != "" {
		args = append(args, "--glob", in.FilePattern)
	}
	args = append(args, in.Pattern, searchDir)

	cmd := exec.CommandContext(ctx, rgPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start ripgrep: %w", err)
	}

	var buf strings.Builder
	matchCount := 0
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		relativeLine := strings.ReplaceAll(line, s.rootDir+string(filepath.Separator), "")
		buf.WriteString(relativeLine + "\n")

		// Count matches for max_results enforcement.
		if in.ContextLines > 0 {
			if line == "--" {
				matchCount++
			}
		} else {
			matchCount++
		}

		emit(ToolEvent{Stage: EventDelta, Content: relativeLine + "\n"})

		if in.MaxResults > 0 && matchCount >= in.MaxResults {
			break
		}
	}

	_ = cmd.Wait()
	return buf.String(), nil
}
```

Note: Add `"bufio"` to imports in `search.go`.

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/tools/ -run "TestSearchTool" -v`
Expected: All PASS (old + new tests)

**Step 5: Commit**

```
[BEHAVIORAL] Implement StreamingTool for SearchTool with line-by-line streaming
```

---

### Task 9: FileTool Streaming Implementation

**Files:**
- Modify: `internal/tools/file.go`
- Modify: `internal/tools/file_test.go`

**Step 1: Write the failing tests**

Add to `internal/tools/file_test.go`:

```go
func TestFileToolImplementsStreamingTool(t *testing.T) {
	ft := NewFileTool(t.TempDir())
	var tool Tool = ft
	_, ok := tool.(StreamingTool)
	assert.True(t, ok, "FileTool should implement StreamingTool")
}

func TestFileToolExecuteStreamRead(t *testing.T) {
	dir := t.TempDir()
	content := "line one\nline two\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.txt"), []byte(content), 0644))

	ft := NewFileTool(dir)
	input, _ := json.Marshal(map[string]string{
		"operation": "read",
		"path":      "test.txt",
	})

	var events []ToolEvent
	result, err := ft.ExecuteStream(context.Background(), input, func(ev ToolEvent) {
		events = append(events, ev)
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, content, result.Content)

	require.GreaterOrEqual(t, len(events), 2)
	assert.Equal(t, EventBegin, events[0].Stage)
	assert.Equal(t, EventEnd, events[len(events)-1].Stage)
}

func TestFileToolExecuteStreamWrite(t *testing.T) {
	dir := t.TempDir()
	ft := NewFileTool(dir)

	input, _ := json.Marshal(map[string]string{
		"operation": "write",
		"path":      "out.txt",
		"content":   "written",
	})

	var events []ToolEvent
	result, err := ft.ExecuteStream(context.Background(), input, func(ev ToolEvent) {
		events = append(events, ev)
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "out.txt")

	require.GreaterOrEqual(t, len(events), 2)
	assert.Equal(t, EventBegin, events[0].Stage)
	assert.Equal(t, EventEnd, events[len(events)-1].Stage)
}

func TestFileToolExecuteStreamInvalidInput(t *testing.T) {
	ft := NewFileTool(t.TempDir())
	result, err := ft.ExecuteStream(context.Background(), json.RawMessage(`{bad`), func(ev ToolEvent) {})
	require.NoError(t, err)
	assert.True(t, result.IsError)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/tools/ -run "TestFileToolImplementsStreaming|TestFileToolExecuteStream" -v`
Expected: FAIL — `ExecuteStream` not defined on FileTool

**Step 3: Write the streaming implementation**

Add `ExecuteStream` to `FileTool` in `internal/tools/file.go`:

```go
// ExecuteStream implements StreamingTool. It emits Begin/End events
// around file operations. For read operations on large files, it
// emits the content as Delta events.
func (f *FileTool) ExecuteStream(_ context.Context, input json.RawMessage, emit func(ToolEvent)) (ToolResult, error) {
	var in fileInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	fullPath, err := f.resolvePath(in.Path)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}

	emit(ToolEvent{Stage: EventBegin, Content: fmt.Sprintf("%s %s\n", in.Operation, in.Path)})

	var result ToolResult
	switch in.Operation {
	case "read":
		result, err = f.readFile(fullPath)
		if err == nil && !result.IsError && len(result.Content) > 4096 {
			// Emit large reads as delta for TUI progress.
			emit(ToolEvent{Stage: EventDelta, Content: result.Content})
		}
	case "write":
		result, err = f.writeFile(fullPath, in.Content)
	case "patch":
		result, err = f.patchFile(fullPath, in.OldString, in.NewString)
	default:
		result = ToolResult{
			Content: fmt.Sprintf("unknown operation: %s", in.Operation),
			IsError: true,
		}
	}

	emit(ToolEvent{Stage: EventEnd, Content: "", IsError: result.IsError})
	return result, err
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/tools/ -run "TestFileTool" -v`
Expected: All PASS

**Step 5: Commit**

```
[BEHAVIORAL] Implement StreamingTool for FileTool with Begin/End events
```

---

### Task 10: Integration Test — Full Streaming Turn

**Files:**
- Modify: `internal/agent/agent_test.go` or `internal/agent/integration_test.go`

**Step 1: Write the integration test**

```go
func TestStreamingToolIntegrationEndToEnd(t *testing.T) {
	// Verify that a streaming tool produces tool_progress events
	// between tool_call and tool_result in a full agent turn.
	streamTool := &mockStreamingTool{
		mockTool: mockTool{
			name:        "stream_cmd",
			description: "streaming command tool",
			inputSchema: json.RawMessage(`{"type":"object","properties":{"cmd":{"type":"string"}}}`),
			executeFn: func(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
				return tools.ToolResult{Content: "sync"}, nil
			},
		},
		streamFn: func(ctx context.Context, input json.RawMessage, emit func(tools.ToolEvent)) (tools.ToolResult, error) {
			emit(tools.ToolEvent{Stage: tools.EventBegin, Content: "begin"})
			emit(tools.ToolEvent{Stage: tools.EventDelta, Content: "progress 1\n"})
			emit(tools.ToolEvent{Stage: tools.EventDelta, Content: "progress 2\n"})
			emit(tools.ToolEvent{Stage: tools.EventEnd, Content: ""})
			return tools.ToolResult{Content: "completed"}, nil
		},
	}

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(streamTool))

	mp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{ID: "tu-1", Name: "stream_cmd"}},
				{Type: "text_delta", Text: `{"cmd":"test"}`},
				{Type: "stop"},
			},
			{
				{Type: "text_delta", Text: "Done."},
				{Type: "stop"},
			},
		},
	}

	cfg := config.DefaultConfig()
	a := New(mp, reg, autoApprove, cfg)

	ch, err := a.Turn(context.Background(), "run stream_cmd")
	require.NoError(t, err)

	var eventTypes []string
	for ev := range ch {
		eventTypes = append(eventTypes, ev.Type)
	}

	// Expected order: tool_call, tool_progress(s), tool_result, text_delta, done
	assert.Contains(t, eventTypes, "tool_call")
	assert.Contains(t, eventTypes, "tool_progress")
	assert.Contains(t, eventTypes, "tool_result")
	assert.Contains(t, eventTypes, "done")

	// tool_progress must come after tool_call and before tool_result.
	toolCallIdx := -1
	firstProgressIdx := -1
	toolResultIdx := -1
	for i, t := range eventTypes {
		if t == "tool_call" && toolCallIdx == -1 {
			toolCallIdx = i
		}
		if t == "tool_progress" && firstProgressIdx == -1 {
			firstProgressIdx = i
		}
		if t == "tool_result" && toolResultIdx == -1 {
			toolResultIdx = i
		}
	}

	assert.Greater(t, firstProgressIdx, toolCallIdx, "tool_progress should come after tool_call")
	assert.Less(t, firstProgressIdx, toolResultIdx, "tool_progress should come before tool_result")
}
```

**Step 2: Run the integration test**

Run: `go test ./internal/agent/ -run TestStreamingToolIntegrationEndToEnd -v`
Expected: PASS

**Step 3: Commit**

```
[BEHAVIORAL] Add end-to-end integration test for streaming tool events
```

---

### Task 11: Final Validation

**Step 1: Run all tests**

Run: `go test ./... -count=1`
Expected: All PASS

**Step 2: Run linter**

Run: `golangci-lint run ./...`
Expected: No errors

**Step 3: Check formatting**

Run: `gofmt -l .`
Expected: No output (all formatted)

**Step 4: Check coverage for changed packages**

Run: `go test -cover ./internal/tools/ ./internal/toolexec/ ./internal/agent/`
Expected: >90% coverage for each package

**Step 5: Final commit if any cleanup needed**

Only if formatting or lint fixes were required.
