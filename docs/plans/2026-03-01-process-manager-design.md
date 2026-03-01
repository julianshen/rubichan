# ProcessManager Design

**Date:** 2026-03-01
**Issue:** #35 — Persistent interactive shell sessions
**Status:** Approved

## Problem

Rubichan's shell tool is fire-and-forget: each `sh -c` invocation is independent. This makes it impossible to start a dev server and interact with it later, use REPLs or interactive debuggers, send input to running processes, or maintain shell state across invocations.

## Design Decisions

- **Separate `process` tool** alongside existing `shell` tool. Shell stays unchanged for simple commands.
- **Pipes with PTY interface** — use `os/exec` pipes internally, but behind a `ProcessIO` interface so we can swap in `creack/pty` later without API changes.
- **Best-effort shutdown with timeout** — SIGTERM with configurable grace period, then SIGKILL. Log any processes that didn't exit cleanly.
- **Unified ProcessTool** — single tool with operation dispatch, backed by a standalone `ProcessManager`.

## Architecture

### ProcessIO Interface

```go
// ProcessIO abstracts the I/O handle for a managed process.
type ProcessIO interface {
    Write(p []byte) (n int, err error) // write to process stdin
    Read(p []byte) (n int, err error)  // read from process output
    Close() error
}
```

The initial implementation uses `os/exec` stdin/stdout pipes. A future PTY implementation satisfies the same interface.

### Core Types

```go
type ProcessStatus int

const (
    ProcessRunning ProcessStatus = iota
    ProcessExited
    ProcessKilled
)

type ManagedProcess struct {
    ID        string
    Command   string
    Cmd       *exec.Cmd
    IO        ProcessIO
    Output    *RingBuffer    // bounded circular buffer of recent output
    Status    ProcessStatus
    ExitCode  int
    StartedAt time.Time
    mu        sync.Mutex     // protects Status, ExitCode, Output reads
}
```

### RingBuffer

Circular byte buffer with configurable capacity (default 64KB). Thread-safe. Supports `Write()` to append and `Bytes()` to read current contents.

### ProcessManager

```go
type ProcessManager struct {
    mu            sync.Mutex
    processes     map[string]*ManagedProcess
    workDir       string
    maxProcesses  int           // default 16
    shutdownGrace time.Duration // default 5s
}
```

**Methods:**
- `Exec(ctx, command) (id, initialOutput, error)` — start process, return ID + first ~1s of output
- `WriteStdin(id, data) error` — send input to a running process
- `ReadOutput(id) (output, status, error)` — get recent output from ring buffer
- `Kill(id) error` — terminate a process
- `List() []ProcessInfo` — list all processes with status
- `Shutdown(ctx) error` — graceful shutdown of all processes

### ProcessTool

Implements `Tool` interface. Dispatches to `ProcessManager` based on `operation` field.

**Input schema:**
```json
{
  "type": "object",
  "properties": {
    "operation": { "enum": ["exec", "write_stdin", "read_output", "kill", "list"] },
    "command":   { "type": "string", "description": "Command to execute (exec only)" },
    "process_id": { "type": "string", "description": "Target process ID" },
    "input":     { "type": "string", "description": "Data to send to stdin (write_stdin only)" }
  },
  "required": ["operation"]
}
```

**Operation behavior:**

| Operation | Required fields | Returns |
|-----------|----------------|---------|
| `exec` | `command` | Process ID + first ~1s of output |
| `write_stdin` | `process_id`, `input` | Confirmation + recent output snippet |
| `read_output` | `process_id` | Last N bytes from ring buffer + status |
| `kill` | `process_id` | Confirmation with exit code |
| `list` | (none) | Table of all processes |

Tool category: `CategoryCore` — always available alongside `shell` and `file`.

## Session Integration

The `ProcessManager` is created during agent initialization and passed to the `ProcessTool`. On session end, the agent calls `ProcessManager.Shutdown()`.

### Shutdown Sequence

1. Send SIGTERM to all running processes
2. Wait up to `ShutdownGrace` (default 5s) for clean exit
3. Send SIGKILL to any still-running processes
4. Log processes that required forced kill
5. Close all I/O handles and ring buffers

### Resource Limits

- Max 16 concurrent processes (configurable). `exec` returns error at limit.
- Ring buffer capped at 64KB per process (worst case total: 1MB).
- One background goroutine per process reads output into ring buffer.

## File Layout

```
internal/tools/
├── process.go              # ProcessTool (Tool interface adapter)
├── process_test.go
├── process_manager.go      # ProcessManager (core logic)
├── process_manager_test.go
├── process_io.go           # ProcessIO interface + pipe implementation
├── process_io_test.go
├── ringbuffer.go           # RingBuffer
└── ringbuffer_test.go
```

## Testing Strategy

- **RingBuffer:** unit tests for write/read, wrap-around, capacity enforcement, concurrent access
- **ProcessIO (pipes):** unit tests for write/read/close, integration test with real process
- **ProcessManager:** unit tests with mock ProcessIO for exec/kill/list/shutdown, integration tests with real processes (echo, sleep, cat)
- **ProcessTool:** unit tests for operation dispatch, input validation, error formatting
- **Agent integration:** test that Shutdown() is called on session end

## LLM Workflow Example

```
LLM: process(exec, "npm run dev")       → pid: "proc_abc123", output: "Server on :3000"
LLM: shell("curl localhost:3000")        → response body
LLM: process(read_output, "proc_abc123") → "Compiled successfully..."
LLM: process(write_stdin, "proc_abc123", "q\n") → "Process exited"
LLM: process(list)                       → (empty)
```
