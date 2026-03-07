# Tool Event Streaming Design

**Date**: 2026-03-07
**Issue**: #33 — Tool event streaming for real-time TUI feedback
**Status**: Approved

## Problem

`Tool.Execute()` is fully synchronous. The TUI shows nothing between `tool_call` and `tool_result` events, leaving users staring at a spinner during long-running operations (builds, test suites, large searches).

## Approach: Channel-Based Result Streaming

The pipeline gains an `ExecuteStream` method that returns a channel of `StreamEvent` values. Callers range over the channel to receive interleaved progress events and a final result. Internally, the emitter function is transported via `context.WithValue` so existing middlewares remain unchanged.

### Why Channel-Based

- Makes the streaming nature explicit at the pipeline boundary
- Callers see a familiar Go pattern (`for ev := range stream`)
- Clean separation: the channel is the public API; context transport is private implementation

## Design

### 1. New Types — `internal/tools/interface.go`

```go
type EventStage int
const (
    EventBegin EventStage = iota
    EventDelta
    EventEnd
)

type ToolEvent struct {
    Stage   EventStage
    Content string
    IsError bool
}

// Optional interface — tools that don't implement this use synchronous Execute().
type StreamingTool interface {
    Tool
    ExecuteStream(ctx context.Context, input json.RawMessage, emit func(ToolEvent)) (ToolResult, error)
}
```

Context helpers for internal emitter transport:

```go
func WithEmitter(ctx context.Context, emit func(ToolEvent)) context.Context
func EmitterFromContext(ctx context.Context) func(ToolEvent)
```

### 2. Pipeline — `internal/toolexec/pipeline.go`

```go
type StreamEventType int
const (
    StreamProgress StreamEventType = iota
    StreamFinal
)

type StreamEvent struct {
    Type   StreamEventType
    Event  *tools.ToolEvent // set when Type == StreamProgress
    Result *Result          // set when Type == StreamFinal
}

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

Existing `Execute` and middleware signatures are unchanged.

### 3. RegistryExecutor — `internal/toolexec/executor.go`

Detects `StreamingTool` and extracts emitter from context:

```go
if st, ok := tool.(tools.StreamingTool); ok {
    emit := tools.EmitterFromContext(ctx)
    if emit == nil {
        emit = func(tools.ToolEvent) {}
    }
    tr, err := st.ExecuteStream(ctx, tc.Input, emit)
    // ... same error/result handling as Execute path
}
```

### 4. Agent Events — `internal/agent/agent.go`

New TurnEvent fields:

```go
type ToolProgressEvent struct {
    ID      string
    Name    string
    Stage   int    // maps to tools.EventStage
    Content string
    IsError bool
}
```

`TurnEvent.Type = "tool_progress"` with `TurnEvent.ToolProgress *ToolProgressEvent`.

`executeSingleTool` gains `ch chan<- TurnEvent` parameter. Calls `pipeline.ExecuteStream` and forwards progress events to the TUI channel:

```go
func (a *Agent) executeSingleTool(ctx context.Context, ch chan<- TurnEvent, tc provider.ToolUseBlock) toolExecResult {
    stream := a.pipeline.ExecuteStream(ctx, toolexec.ToolCall{
        ID: tc.ID, Name: tc.Name, Input: tc.Input,
    })
    var finalResult toolexec.Result
    for ev := range stream {
        switch ev.Type {
        case toolexec.StreamProgress:
            ch <- TurnEvent{
                Type: "tool_progress",
                ToolProgress: &ToolProgressEvent{
                    ID: tc.ID, Name: tc.Name,
                    Stage: int(ev.Event.Stage), Content: ev.Event.Content,
                    IsError: ev.Event.IsError,
                },
            }
        case toolexec.StreamFinal:
            finalResult = *ev.Result
        }
    }
    return toolExecResult{
        toolUseID: tc.ID, content: finalResult.Content,
        isError: finalResult.IsError,
        event: makeToolResultEvent(tc.ID, tc.Name, finalResult.Content, finalResult.DisplayContent, finalResult.IsError),
    }
}
```

Parallel execution: progress events stream in real-time (interleaved — each event carries tool ID/name for disambiguation). Final results are batched in order after all parallel tools complete.

### 5. TUI Rendering — `internal/tui/update.go`

```go
case "tool_progress":
    if msg.ToolProgress != nil {
        m.content.WriteString(msg.ToolProgress.Content)
        m.setContentAndAutoScroll(m.content.String())
    }
    return m, m.waitForEvent()
```

### 6. Streaming Tool Implementations

**ShellTool** (`internal/tools/shell.go`):
- Uses `cmd.StdoutPipe()` + `cmd.StderrPipe()` with line-by-line scanning
- Emits `EventBegin` with command info before execution
- Emits `EventDelta` per stdout/stderr line
- Emits `EventEnd` with exit status
- Accumulates all output for the final `ToolResult`
- Safety interceptors, diff tracking, timeout handling preserved

**SearchTool** (`internal/tools/search.go`):
- Ripgrep path: `cmd.StdoutPipe()` with line-by-line emission
- Go-native path: emits per-file match blocks as `EventDelta`
- Both return complete result for LLM conversation

**FileTool** (`internal/tools/file.go`):
- `EventBegin` with operation + path
- For large reads (>4KB): emit chunks as `EventDelta`
- `EventEnd` with result summary

## Backward Compatibility

- `StreamingTool` is optional; non-streaming tools continue using `Execute()`
- `Pipeline.Execute()` unchanged; `ExecuteStream` is additive
- All middleware signatures preserved
- `HandlerFunc` type unchanged
- Existing tests pass without modification

## Testing Strategy

- Unit tests for `ToolEvent`, `StreamingTool` interface detection
- Unit tests for `Pipeline.ExecuteStream` with both streaming and non-streaming tools
- Unit tests for `RegistryExecutor` streaming path
- Integration test: streaming shell tool emits Begin/Delta/End events
- Integration test: agent turn with streaming tool produces `tool_progress` TurnEvents
- TUI test: `tool_progress` events render incrementally

## Files Changed

| File | Change |
|------|--------|
| `internal/tools/interface.go` | Add `EventStage`, `ToolEvent`, `StreamingTool`, context helpers |
| `internal/tools/interface_test.go` | Tests for new types and context helpers |
| `internal/toolexec/pipeline.go` | Add `StreamEvent`, `ExecuteStream` |
| `internal/toolexec/pipeline_test.go` | Tests for `ExecuteStream` |
| `internal/toolexec/executor.go` | StreamingTool detection in RegistryExecutor |
| `internal/toolexec/executor_test.go` | Tests for streaming executor path |
| `internal/agent/agent.go` | `ToolProgressEvent`, `executeSingleTool` changes |
| `internal/agent/agent_test.go` | Integration tests for streaming turn events |
| `internal/tui/update.go` | Handle `tool_progress` event type |
| `internal/tui/update_test.go` | Tests for progress event rendering |
| `internal/tools/shell.go` | Implement `StreamingTool` |
| `internal/tools/shell_test.go` | Tests for streaming shell execution |
| `internal/tools/search.go` | Implement `StreamingTool` |
| `internal/tools/search_test.go` | Tests for streaming search |
| `internal/tools/file.go` | Implement `StreamingTool` |
| `internal/tools/file_test.go` | Tests for streaming file operations |
