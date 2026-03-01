# ProcessManager Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement persistent interactive shell sessions via a ProcessManager that maintains long-running processes across multiple tool invocations.

**Architecture:** A standalone `ProcessManager` manages process lifecycle (exec, I/O, kill, shutdown) behind a `ProcessIO` interface (pipes now, PTY later). A `ProcessTool` implements the `Tool` interface and dispatches operations to the manager. The manager integrates into the agent lifecycle for cleanup on session end.

**Tech Stack:** Go stdlib (`os/exec`, `io`, `sync`), `google/uuid` for process IDs, `stretchr/testify` for tests.

---

### Task 1: RingBuffer — basic write and read

**Files:**
- Create: `internal/tools/ringbuffer.go`
- Create: `internal/tools/ringbuffer_test.go`

**Step 1: Write the failing test**

```go
package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRingBufferWriteAndRead(t *testing.T) {
	rb := NewRingBuffer(64)
	n, err := rb.Write([]byte("hello"))
	assert.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, []byte("hello"), rb.Bytes())
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/ -run TestRingBufferWriteAndRead -v`
Expected: FAIL — `NewRingBuffer` undefined

**Step 3: Write minimal implementation**

```go
package tools

import "sync"

// RingBuffer is a fixed-capacity circular byte buffer. Writes that exceed
// capacity overwrite the oldest data. All methods are safe for concurrent use.
type RingBuffer struct {
	mu   sync.Mutex
	buf  []byte
	cap  int
	pos  int // next write position
	full bool
}

// NewRingBuffer creates a RingBuffer with the given byte capacity.
func NewRingBuffer(capacity int) *RingBuffer {
	return &RingBuffer{
		buf: make([]byte, capacity),
		cap: capacity,
	}
}

// Write appends p to the buffer, overwriting oldest data if capacity is exceeded.
func (r *RingBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	n := len(p)
	if n >= r.cap {
		// Data larger than buffer — keep only the last cap bytes.
		copy(r.buf, p[n-r.cap:])
		r.pos = 0
		r.full = true
		return n, nil
	}

	for i := 0; i < n; i++ {
		r.buf[r.pos] = p[i]
		r.pos = (r.pos + 1) % r.cap
	}
	if r.pos <= len(p)-1 && len(p) > 0 {
		// We've wrapped around at least once during this write
		r.full = true
	}
	// Check if total written has filled the buffer
	if !r.full && r.pos == 0 && n > 0 {
		r.full = true
	}

	return n, nil
}

// Bytes returns the buffered content in order from oldest to newest.
func (r *RingBuffer) Bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.full {
		out := make([]byte, r.pos)
		copy(out, r.buf[:r.pos])
		return out
	}

	out := make([]byte, r.cap)
	copy(out, r.buf[r.pos:])
	copy(out[r.cap-r.pos:], r.buf[:r.pos])
	return out
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/ -run TestRingBufferWriteAndRead -v`
Expected: PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add RingBuffer with basic write and read
```

---

### Task 2: RingBuffer — wrap-around behavior

**Files:**
- Modify: `internal/tools/ringbuffer_test.go`

**Step 1: Write the failing test**

```go
func TestRingBufferWrapAround(t *testing.T) {
	rb := NewRingBuffer(8)
	rb.Write([]byte("abcdefgh")) // fills exactly
	rb.Write([]byte("ij"))       // overwrites first 2 bytes

	got := rb.Bytes()
	assert.Equal(t, []byte("cdefghij"), got)
}

func TestRingBufferOverflow(t *testing.T) {
	rb := NewRingBuffer(4)
	rb.Write([]byte("abcdefghij")) // 10 bytes into 4-byte buffer

	got := rb.Bytes()
	assert.Equal(t, []byte("ghij"), got) // only last 4 bytes survive
}
```

**Step 2: Run tests to verify they fail or pass**

Run: `go test ./internal/tools/ -run "TestRingBuffer" -v`
Expected: Both should PASS if implementation is correct. If they fail, fix the `Write` logic.

**Step 3: Fix implementation if needed**

The wrap-around tracking in `Write` may need adjustment. If tests fail, simplify the full-detection:

```go
// In Write, after the byte-by-byte copy loop, replace the full-detection with:
if !r.full {
    // We started at some position and wrote n bytes.
    // If we wrapped around (new pos <= old start pos), buffer is full.
    // Simpler: track total written.
}
```

**Step 4: Run tests**

Run: `go test ./internal/tools/ -run "TestRingBuffer" -v`
Expected: ALL PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add RingBuffer wrap-around and overflow tests
```

---

### Task 3: RingBuffer — Reset and Len

**Files:**
- Modify: `internal/tools/ringbuffer.go`
- Modify: `internal/tools/ringbuffer_test.go`

**Step 1: Write the failing tests**

```go
func TestRingBufferLen(t *testing.T) {
	rb := NewRingBuffer(16)
	assert.Equal(t, 0, rb.Len())

	rb.Write([]byte("hello"))
	assert.Equal(t, 5, rb.Len())

	rb.Write([]byte("worldworld12")) // 12 more, total 17 > cap 16
	assert.Equal(t, 16, rb.Len())    // capped at capacity
}

func TestRingBufferReset(t *testing.T) {
	rb := NewRingBuffer(16)
	rb.Write([]byte("data"))
	rb.Reset()

	assert.Equal(t, 0, rb.Len())
	assert.Empty(t, rb.Bytes())
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/tools/ -run "TestRingBuffer(Len|Reset)" -v`
Expected: FAIL — `Len` and `Reset` undefined

**Step 3: Write minimal implementation**

Add to `ringbuffer.go`:

```go
// Len returns the number of bytes currently stored.
func (r *RingBuffer) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.full {
		return r.cap
	}
	return r.pos
}

// Reset clears the buffer.
func (r *RingBuffer) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.pos = 0
	r.full = false
}
```

**Step 4: Run tests**

Run: `go test ./internal/tools/ -run "TestRingBuffer" -v`
Expected: ALL PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add RingBuffer Len and Reset methods
```

---

### Task 4: ProcessIO interface and pipe implementation

**Files:**
- Create: `internal/tools/process_io.go`
- Create: `internal/tools/process_io_test.go`

**Step 1: Write the failing test**

```go
package tools

import (
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPipeProcessIOWriteAndRead(t *testing.T) {
	// Use "cat" which echoes stdin to stdout.
	cmd := exec.Command("cat")
	pio, err := NewPipeProcessIO(cmd)
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	defer cmd.Process.Kill()

	_, err = pio.Write([]byte("hello\n"))
	require.NoError(t, err)

	buf := make([]byte, 64)
	// Give cat a moment to echo back
	time.Sleep(50 * time.Millisecond)
	n, err := pio.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "hello\n", string(buf[:n]))

	require.NoError(t, pio.Close())
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/ -run TestPipeProcessIOWriteAndRead -v`
Expected: FAIL — `ProcessIO` and `NewPipeProcessIO` undefined

**Step 3: Write minimal implementation**

```go
package tools

import (
	"io"
	"os/exec"
)

// ProcessIO abstracts the I/O handle for a managed process.
// The pipe implementation uses os/exec stdin/stdout pipes.
// A future PTY implementation can satisfy the same interface.
type ProcessIO interface {
	Write(p []byte) (n int, err error)
	Read(p []byte) (n int, err error)
	Close() error
}

// PipeProcessIO implements ProcessIO using os/exec stdin/stdout pipes.
type PipeProcessIO struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
}

// NewPipeProcessIO creates pipes for the given command. Must be called
// before cmd.Start().
func NewPipeProcessIO(cmd *exec.Cmd) (*PipeProcessIO, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}
	// Merge stderr into stdout so we capture all output.
	cmd.Stderr = cmd.Stdout

	return &PipeProcessIO{stdin: stdin, stdout: stdout}, nil
}

func (p *PipeProcessIO) Write(data []byte) (int, error) {
	return p.stdin.Write(data)
}

func (p *PipeProcessIO) Read(buf []byte) (int, error) {
	return p.stdout.Read(buf)
}

func (p *PipeProcessIO) Close() error {
	_ = p.stdin.Close()
	return p.stdout.Close()
}
```

**Step 4: Run test**

Run: `go test ./internal/tools/ -run TestPipeProcessIO -v`
Expected: PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add ProcessIO interface and PipeProcessIO implementation
```

---

### Task 5: ProcessIO — close behavior

**Files:**
- Modify: `internal/tools/process_io_test.go`

**Step 1: Write the failing test**

```go
func TestPipeProcessIOCloseSignalsEOF(t *testing.T) {
	cmd := exec.Command("cat")
	pio, err := NewPipeProcessIO(cmd)
	require.NoError(t, err)
	require.NoError(t, cmd.Start())

	// Close stdin — cat should exit, and reads should return EOF.
	require.NoError(t, pio.Close())
	_ = cmd.Wait()

	buf := make([]byte, 64)
	_, err = pio.Read(buf)
	assert.ErrorIs(t, err, io.EOF)
}
```

**Step 2: Run test**

Run: `go test ./internal/tools/ -run TestPipeProcessIOCloseSignalsEOF -v`
Expected: Should PASS with current implementation (closing stdin causes cat to exit and EOF on stdout). If it fails, adjust Close to handle the EOF case.

**Step 3: Fix if needed**

No implementation change expected. This test validates the pipe behavior.

**Step 4: Run all ProcessIO tests**

Run: `go test ./internal/tools/ -run TestPipeProcessIO -v`
Expected: ALL PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add ProcessIO close-signals-EOF test
```

---

### Task 6: ProcessManager — types and constructor

**Files:**
- Create: `internal/tools/process_manager.go`
- Create: `internal/tools/process_manager_test.go`

**Step 1: Write the failing test**

```go
package tools

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewProcessManager(t *testing.T) {
	dir := t.TempDir()
	pm := NewProcessManager(dir, ProcessManagerConfig{
		MaxProcesses:  8,
		BufferSize:    1024,
		ShutdownGrace: 3 * time.Second,
	})

	assert.NotNil(t, pm)
	assert.Empty(t, pm.List())
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/ -run TestNewProcessManager -v`
Expected: FAIL — types undefined

**Step 3: Write minimal implementation**

```go
package tools

import (
	"sync"
	"time"
)

// ProcessStatus represents the lifecycle state of a managed process.
type ProcessStatus int

const (
	ProcessRunning ProcessStatus = iota
	ProcessExited
	ProcessKilled
)

// String returns a human-readable status label.
func (s ProcessStatus) String() string {
	switch s {
	case ProcessRunning:
		return "running"
	case ProcessExited:
		return "exited"
	case ProcessKilled:
		return "killed"
	default:
		return "unknown"
	}
}

// ProcessInfo is the read-only view of a managed process returned by List.
type ProcessInfo struct {
	ID        string
	Command   string
	Status    ProcessStatus
	ExitCode  int
	StartedAt time.Time
}

// ProcessManagerConfig holds configuration for a ProcessManager.
type ProcessManagerConfig struct {
	MaxProcesses  int
	BufferSize    int
	ShutdownGrace time.Duration
}

// ProcessManager maintains long-running processes across multiple tool
// invocations. All methods are safe for concurrent use.
type ProcessManager struct {
	mu            sync.Mutex
	processes     map[string]*managedProcess
	workDir       string
	maxProcesses  int
	bufferSize    int
	shutdownGrace time.Duration
}

// managedProcess is the internal representation of a running process.
type managedProcess struct {
	id        string
	command   string
	io        ProcessIO
	output    *RingBuffer
	status    ProcessStatus
	exitCode  int
	startedAt time.Time
	mu        sync.Mutex
	done      chan struct{} // closed when process exits
}

// NewProcessManager creates a ProcessManager with the given configuration.
func NewProcessManager(workDir string, cfg ProcessManagerConfig) *ProcessManager {
	if cfg.MaxProcesses <= 0 {
		cfg.MaxProcesses = 16
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 64 * 1024
	}
	if cfg.ShutdownGrace <= 0 {
		cfg.ShutdownGrace = 5 * time.Second
	}
	return &ProcessManager{
		processes:     make(map[string]*managedProcess),
		workDir:       workDir,
		maxProcesses:  cfg.MaxProcesses,
		bufferSize:    cfg.BufferSize,
		shutdownGrace: cfg.ShutdownGrace,
	}
}

// List returns info about all managed processes.
func (pm *ProcessManager) List() []ProcessInfo {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	infos := make([]ProcessInfo, 0, len(pm.processes))
	for _, p := range pm.processes {
		p.mu.Lock()
		infos = append(infos, ProcessInfo{
			ID:        p.id,
			Command:   p.command,
			Status:    p.status,
			ExitCode:  p.exitCode,
			StartedAt: p.startedAt,
		})
		p.mu.Unlock()
	}
	return infos
}
```

**Step 4: Run test**

Run: `go test ./internal/tools/ -run TestNewProcessManager -v`
Expected: PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add ProcessManager types, constructor, and List
```

---

### Task 7: ProcessManager — Exec (start a process)

**Files:**
- Modify: `internal/tools/process_manager.go`
- Modify: `internal/tools/process_manager_test.go`

**Step 1: Write the failing test**

```go
func TestProcessManagerExec(t *testing.T) {
	dir := t.TempDir()
	pm := NewProcessManager(dir, ProcessManagerConfig{
		MaxProcesses: 4,
		BufferSize:   1024,
	})
	defer pm.Shutdown(context.Background())

	ctx := context.Background()
	id, output, err := pm.Exec(ctx, "echo hello")
	require.NoError(t, err)
	assert.NotEmpty(t, id)
	assert.Contains(t, output, "hello")

	// Process should appear in list (may be exited already since echo is fast).
	procs := pm.List()
	assert.Len(t, procs, 1)
	assert.Equal(t, id, procs[0].ID)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/ -run TestProcessManagerExec -v`
Expected: FAIL — `Exec` and `Shutdown` undefined

**Step 3: Write minimal implementation**

Add to `process_manager.go`:

```go
import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/google/uuid"
)

// Exec starts a new process and returns its ID and initial output.
// It waits briefly (up to 1 second) to capture startup output.
func (pm *ProcessManager) Exec(ctx context.Context, command string) (string, string, error) {
	pm.mu.Lock()
	if len(pm.processes) >= pm.maxProcesses {
		pm.mu.Unlock()
		return "", "", fmt.Errorf("process limit reached (%d)", pm.maxProcesses)
	}
	pm.mu.Unlock()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = pm.workDir

	pio, err := NewPipeProcessIO(cmd)
	if err != nil {
		return "", "", fmt.Errorf("creating process I/O: %w", err)
	}

	if err := cmd.Start(); err != nil {
		pio.Close()
		return "", "", fmt.Errorf("starting process: %w", err)
	}

	id := uuid.New().String()[:8]
	proc := &managedProcess{
		id:        id,
		command:   command,
		io:        pio,
		output:    NewRingBuffer(pm.bufferSize),
		status:    ProcessRunning,
		startedAt: time.Now(),
		done:      make(chan struct{}),
	}

	pm.mu.Lock()
	pm.processes[id] = proc
	pm.mu.Unlock()

	// Background goroutine reads process output into ring buffer.
	go pm.readLoop(proc)

	// Background goroutine waits for process exit.
	go pm.waitLoop(proc, cmd)

	// Wait briefly for initial output.
	select {
	case <-proc.done:
		// Process already exited (e.g., echo).
	case <-time.After(1 * time.Second):
		// Timeout — return what we have so far.
	case <-ctx.Done():
		return id, "", ctx.Err()
	}

	return id, string(proc.output.Bytes()), nil
}

// readLoop continuously reads from the process I/O into the ring buffer.
func (pm *ProcessManager) readLoop(proc *managedProcess) {
	buf := make([]byte, 4096)
	for {
		n, err := proc.io.Read(buf)
		if n > 0 {
			proc.output.Write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// waitLoop waits for the process to exit and updates its status.
func (pm *ProcessManager) waitLoop(proc *managedProcess, cmd *exec.Cmd) {
	err := cmd.Wait()

	proc.mu.Lock()
	if proc.status == ProcessRunning {
		proc.status = ProcessExited
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			proc.exitCode = exitErr.ExitCode()
		}
	}
	proc.mu.Unlock()

	close(proc.done)
}

// Shutdown terminates all running processes with a grace period.
func (pm *ProcessManager) Shutdown(ctx context.Context) error {
	pm.mu.Lock()
	procs := make([]*managedProcess, 0, len(pm.processes))
	for _, p := range pm.processes {
		procs = append(procs, p)
	}
	pm.mu.Unlock()

	for _, p := range procs {
		pm.killProcess(p)
	}
	return nil
}

// killProcess sends SIGTERM, waits for grace period, then SIGKILL.
func (pm *ProcessManager) killProcess(proc *managedProcess) {
	proc.mu.Lock()
	if proc.status != ProcessRunning {
		proc.mu.Unlock()
		return
	}
	proc.mu.Unlock()

	proc.io.Close()

	select {
	case <-proc.done:
		return // exited cleanly
	case <-time.After(pm.shutdownGrace):
	}

	proc.mu.Lock()
	proc.status = ProcessKilled
	proc.mu.Unlock()
}
```

**Step 4: Run test**

Run: `go test ./internal/tools/ -run TestProcessManagerExec -v`
Expected: PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add ProcessManager Exec with background read and wait loops
```

---

### Task 8: ProcessManager — Exec limit enforcement

**Files:**
- Modify: `internal/tools/process_manager_test.go`

**Step 1: Write the failing test**

```go
func TestProcessManagerExecLimit(t *testing.T) {
	dir := t.TempDir()
	pm := NewProcessManager(dir, ProcessManagerConfig{
		MaxProcesses: 2,
		BufferSize:   1024,
	})
	defer pm.Shutdown(context.Background())

	ctx := context.Background()
	_, _, err := pm.Exec(ctx, "sleep 10")
	require.NoError(t, err)
	_, _, err = pm.Exec(ctx, "sleep 10")
	require.NoError(t, err)

	// Third should fail.
	_, _, err = pm.Exec(ctx, "sleep 10")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "process limit")
}
```

**Step 2: Run test**

Run: `go test ./internal/tools/ -run TestProcessManagerExecLimit -v`
Expected: PASS (limit check already in Exec). If FAIL, fix the limit logic.

**Step 3: Commit (if test passes)**

```
[BEHAVIORAL] Add ProcessManager exec limit test
```

---

### Task 9: ProcessManager — ReadOutput

**Files:**
- Modify: `internal/tools/process_manager.go`
- Modify: `internal/tools/process_manager_test.go`

**Step 1: Write the failing test**

```go
func TestProcessManagerReadOutput(t *testing.T) {
	dir := t.TempDir()
	pm := NewProcessManager(dir, ProcessManagerConfig{BufferSize: 1024})
	defer pm.Shutdown(context.Background())

	ctx := context.Background()
	id, _, err := pm.Exec(ctx, "echo line1; echo line2")
	require.NoError(t, err)

	// Wait for process to finish so all output is captured.
	time.Sleep(200 * time.Millisecond)

	output, status, err := pm.ReadOutput(id)
	require.NoError(t, err)
	assert.Contains(t, output, "line1")
	assert.Contains(t, output, "line2")
	assert.Equal(t, ProcessExited, status)
}

func TestProcessManagerReadOutputUnknownID(t *testing.T) {
	dir := t.TempDir()
	pm := NewProcessManager(dir, ProcessManagerConfig{})

	_, _, err := pm.ReadOutput("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/ -run "TestProcessManagerReadOutput" -v`
Expected: FAIL — `ReadOutput` undefined

**Step 3: Write minimal implementation**

```go
// ReadOutput returns the recent output and current status of a process.
func (pm *ProcessManager) ReadOutput(id string) (string, ProcessStatus, error) {
	pm.mu.Lock()
	proc, ok := pm.processes[id]
	pm.mu.Unlock()

	if !ok {
		return "", 0, fmt.Errorf("process not found: %s", id)
	}

	proc.mu.Lock()
	status := proc.status
	proc.mu.Unlock()

	return string(proc.output.Bytes()), status, nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/tools/ -run "TestProcessManagerReadOutput" -v`
Expected: ALL PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add ProcessManager ReadOutput method
```

---

### Task 10: ProcessManager — WriteStdin

**Files:**
- Modify: `internal/tools/process_manager.go`
- Modify: `internal/tools/process_manager_test.go`

**Step 1: Write the failing test**

```go
func TestProcessManagerWriteStdin(t *testing.T) {
	dir := t.TempDir()
	pm := NewProcessManager(dir, ProcessManagerConfig{BufferSize: 1024})
	defer pm.Shutdown(context.Background())

	ctx := context.Background()
	// Start cat, which echoes stdin to stdout.
	id, _, err := pm.Exec(ctx, "cat")
	require.NoError(t, err)

	err = pm.WriteStdin(id, "hello from stdin\n")
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)

	output, status, err := pm.ReadOutput(id)
	require.NoError(t, err)
	assert.Contains(t, output, "hello from stdin")
	assert.Equal(t, ProcessRunning, status)
}

func TestProcessManagerWriteStdinExitedProcess(t *testing.T) {
	dir := t.TempDir()
	pm := NewProcessManager(dir, ProcessManagerConfig{BufferSize: 1024})
	defer pm.Shutdown(context.Background())

	ctx := context.Background()
	id, _, err := pm.Exec(ctx, "echo done")
	require.NoError(t, err)

	// Wait for it to exit.
	time.Sleep(200 * time.Millisecond)

	err = pm.WriteStdin(id, "too late\n")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/ -run "TestProcessManagerWriteStdin" -v`
Expected: FAIL — `WriteStdin` undefined

**Step 3: Write minimal implementation**

```go
// WriteStdin sends data to a running process's standard input.
func (pm *ProcessManager) WriteStdin(id string, data string) error {
	pm.mu.Lock()
	proc, ok := pm.processes[id]
	pm.mu.Unlock()

	if !ok {
		return fmt.Errorf("process not found: %s", id)
	}

	proc.mu.Lock()
	status := proc.status
	proc.mu.Unlock()

	if status != ProcessRunning {
		return fmt.Errorf("process %s is not running (status: %s)", id, status)
	}

	_, err := proc.io.Write([]byte(data))
	if err != nil {
		return fmt.Errorf("writing to process %s: %w", id, err)
	}
	return nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/tools/ -run "TestProcessManagerWriteStdin" -v`
Expected: ALL PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add ProcessManager WriteStdin method
```

---

### Task 11: ProcessManager — Kill

**Files:**
- Modify: `internal/tools/process_manager.go`
- Modify: `internal/tools/process_manager_test.go`

**Step 1: Write the failing test**

```go
func TestProcessManagerKill(t *testing.T) {
	dir := t.TempDir()
	pm := NewProcessManager(dir, ProcessManagerConfig{
		BufferSize:    1024,
		ShutdownGrace: 1 * time.Second,
	})
	defer pm.Shutdown(context.Background())

	ctx := context.Background()
	id, _, err := pm.Exec(ctx, "sleep 60")
	require.NoError(t, err)

	err = pm.Kill(id)
	require.NoError(t, err)

	// Verify it's no longer running.
	_, status, err := pm.ReadOutput(id)
	require.NoError(t, err)
	assert.NotEqual(t, ProcessRunning, status)
}

func TestProcessManagerKillUnknownID(t *testing.T) {
	dir := t.TempDir()
	pm := NewProcessManager(dir, ProcessManagerConfig{})

	err := pm.Kill("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/ -run "TestProcessManagerKill" -v`
Expected: FAIL — `Kill` undefined

**Step 3: Write minimal implementation**

```go
// Kill terminates a running process.
func (pm *ProcessManager) Kill(id string) error {
	pm.mu.Lock()
	proc, ok := pm.processes[id]
	pm.mu.Unlock()

	if !ok {
		return fmt.Errorf("process not found: %s", id)
	}

	pm.killProcess(proc)
	return nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/tools/ -run "TestProcessManagerKill" -v`
Expected: ALL PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add ProcessManager Kill method
```

---

### Task 12: ProcessManager — Shutdown with grace period

**Files:**
- Modify: `internal/tools/process_manager_test.go`

**Step 1: Write the failing test**

```go
func TestProcessManagerShutdown(t *testing.T) {
	dir := t.TempDir()
	pm := NewProcessManager(dir, ProcessManagerConfig{
		MaxProcesses:  4,
		BufferSize:    1024,
		ShutdownGrace: 1 * time.Second,
	})

	ctx := context.Background()
	id1, _, err := pm.Exec(ctx, "sleep 60")
	require.NoError(t, err)
	id2, _, err := pm.Exec(ctx, "sleep 60")
	require.NoError(t, err)

	err = pm.Shutdown(ctx)
	require.NoError(t, err)

	// Both processes should be terminated.
	_, s1, _ := pm.ReadOutput(id1)
	_, s2, _ := pm.ReadOutput(id2)
	assert.NotEqual(t, ProcessRunning, s1)
	assert.NotEqual(t, ProcessRunning, s2)
}
```

**Step 2: Run test**

Run: `go test ./internal/tools/ -run TestProcessManagerShutdown -v`
Expected: PASS (Shutdown already implemented). If FAIL, fix the shutdown sequence.

**Step 3: Commit**

```
[BEHAVIORAL] Add ProcessManager Shutdown integration test
```

---

### Task 13: ProcessTool — operation dispatch and metadata

**Files:**
- Create: `internal/tools/process.go`
- Create: `internal/tools/process_test.go`

**Step 1: Write the failing test**

```go
package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessToolMetadata(t *testing.T) {
	dir := t.TempDir()
	pm := NewProcessManager(dir, ProcessManagerConfig{BufferSize: 1024})
	pt := NewProcessTool(pm)

	assert.Equal(t, "process", pt.Name())
	assert.NotEmpty(t, pt.Description())

	var schema map[string]any
	require.NoError(t, json.Unmarshal(pt.InputSchema(), &schema))
	props := schema["properties"].(map[string]any)
	assert.Contains(t, props, "operation")
	assert.Contains(t, props, "command")
	assert.Contains(t, props, "process_id")
	assert.Contains(t, props, "input")
}

func TestProcessToolInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	pm := NewProcessManager(dir, ProcessManagerConfig{BufferSize: 1024})
	pt := NewProcessTool(pm)

	result, err := pt.Execute(context.Background(), json.RawMessage(`{bad`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid input")
}

func TestProcessToolUnknownOperation(t *testing.T) {
	dir := t.TempDir()
	pm := NewProcessManager(dir, ProcessManagerConfig{BufferSize: 1024})
	pt := NewProcessTool(pm)

	input, _ := json.Marshal(map[string]string{"operation": "bogus"})
	result, err := pt.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unknown operation")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/ -run "TestProcessTool" -v`
Expected: FAIL — `NewProcessTool` undefined

**Step 3: Write minimal implementation**

```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// processInput represents the input for the process tool.
type processInput struct {
	Operation string `json:"operation"`
	Command   string `json:"command,omitempty"`
	ProcessID string `json:"process_id,omitempty"`
	Input     string `json:"input,omitempty"`
}

// ProcessTool manages long-running processes via a ProcessManager.
// It implements the Tool interface with operation-based dispatch.
type ProcessTool struct {
	manager *ProcessManager
}

// NewProcessTool creates a ProcessTool backed by the given ProcessManager.
func NewProcessTool(manager *ProcessManager) *ProcessTool {
	return &ProcessTool{manager: manager}
}

func (p *ProcessTool) Name() string { return "process" }

func (p *ProcessTool) Description() string {
	return "Manage long-running processes. Operations: exec (start), write_stdin (send input), read_output (get output), kill (terminate), list (show all)."
}

func (p *ProcessTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"operation": {
				"type": "string",
				"enum": ["exec", "write_stdin", "read_output", "kill", "list"],
				"description": "The operation to perform"
			},
			"command": {
				"type": "string",
				"description": "The command to execute (exec operation only)"
			},
			"process_id": {
				"type": "string",
				"description": "Target process ID (write_stdin, read_output, kill)"
			},
			"input": {
				"type": "string",
				"description": "Data to send to stdin (write_stdin operation only)"
			}
		},
		"required": ["operation"]
	}`)
}

func (p *ProcessTool) Execute(ctx context.Context, input json.RawMessage) (ToolResult, error) {
	var in processInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	switch in.Operation {
	case "exec":
		return p.execOp(ctx, in)
	case "write_stdin":
		return p.writeStdinOp(in)
	case "read_output":
		return p.readOutputOp(in)
	case "kill":
		return p.killOp(in)
	case "list":
		return p.listOp()
	default:
		return ToolResult{
			Content: fmt.Sprintf("unknown operation: %s", in.Operation),
			IsError: true,
		}, nil
	}
}

func (p *ProcessTool) execOp(_ context.Context, _ processInput) (ToolResult, error) {
	return ToolResult{Content: "not implemented", IsError: true}, nil
}

func (p *ProcessTool) writeStdinOp(_ processInput) (ToolResult, error) {
	return ToolResult{Content: "not implemented", IsError: true}, nil
}

func (p *ProcessTool) readOutputOp(_ processInput) (ToolResult, error) {
	return ToolResult{Content: "not implemented", IsError: true}, nil
}

func (p *ProcessTool) killOp(_ processInput) (ToolResult, error) {
	return ToolResult{Content: "not implemented", IsError: true}, nil
}

func (p *ProcessTool) listOp() (ToolResult, error) {
	return ToolResult{Content: "not implemented", IsError: true}, nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/tools/ -run "TestProcessTool" -v`
Expected: ALL PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add ProcessTool with operation dispatch skeleton
```

---

### Task 14: ProcessTool — exec operation

**Files:**
- Modify: `internal/tools/process.go`
- Modify: `internal/tools/process_test.go`

**Step 1: Write the failing test**

```go
func TestProcessToolExec(t *testing.T) {
	dir := t.TempDir()
	pm := NewProcessManager(dir, ProcessManagerConfig{BufferSize: 1024})
	pt := NewProcessTool(pm)
	defer pm.Shutdown(context.Background())

	input, _ := json.Marshal(map[string]string{
		"operation": "exec",
		"command":   "echo hello from process",
	})
	result, err := pt.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "hello from process")
	assert.Contains(t, result.Content, "process_id")
}

func TestProcessToolExecMissingCommand(t *testing.T) {
	dir := t.TempDir()
	pm := NewProcessManager(dir, ProcessManagerConfig{BufferSize: 1024})
	pt := NewProcessTool(pm)

	input, _ := json.Marshal(map[string]string{"operation": "exec"})
	result, err := pt.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "command is required")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/ -run "TestProcessToolExec" -v`
Expected: FAIL — exec returns "not implemented"

**Step 3: Implement exec operation**

Replace `execOp` in `process.go`:

```go
func (p *ProcessTool) execOp(ctx context.Context, in processInput) (ToolResult, error) {
	if in.Command == "" {
		return ToolResult{Content: "command is required for exec operation", IsError: true}, nil
	}

	id, output, err := p.manager.Exec(ctx, in.Command)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("exec failed: %s", err), IsError: true}, nil
	}

	content := fmt.Sprintf("process_id: %s\n%s", id, output)
	return ToolResult{Content: content}, nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/tools/ -run "TestProcessToolExec" -v`
Expected: ALL PASS

**Step 5: Commit**

```
[BEHAVIORAL] Implement ProcessTool exec operation
```

---

### Task 15: ProcessTool — read_output operation

**Files:**
- Modify: `internal/tools/process.go`
- Modify: `internal/tools/process_test.go`

**Step 1: Write the failing test**

```go
func TestProcessToolReadOutput(t *testing.T) {
	dir := t.TempDir()
	pm := NewProcessManager(dir, ProcessManagerConfig{BufferSize: 1024})
	pt := NewProcessTool(pm)
	defer pm.Shutdown(context.Background())

	// Start a process.
	execInput, _ := json.Marshal(map[string]string{
		"operation": "exec",
		"command":   "echo output-line",
	})
	execResult, _ := pt.Execute(context.Background(), execInput)
	require.False(t, execResult.IsError)

	time.Sleep(200 * time.Millisecond)

	// Extract process_id from exec output (first line: "process_id: xxx")
	// For simplicity, use the manager's List.
	procs := pm.List()
	require.Len(t, procs, 1)

	readInput, _ := json.Marshal(map[string]string{
		"operation":  "read_output",
		"process_id": procs[0].ID,
	})
	result, err := pt.Execute(context.Background(), readInput)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "output-line")
	assert.Contains(t, result.Content, "status:")
}
```

**Step 2: Run test — should fail (returns "not implemented")**

Run: `go test ./internal/tools/ -run TestProcessToolReadOutput -v`

**Step 3: Implement**

```go
func (p *ProcessTool) readOutputOp(in processInput) (ToolResult, error) {
	if in.ProcessID == "" {
		return ToolResult{Content: "process_id is required for read_output", IsError: true}, nil
	}

	output, status, err := p.manager.ReadOutput(in.ProcessID)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("read_output failed: %s", err), IsError: true}, nil
	}

	content := fmt.Sprintf("status: %s\n%s", status, output)
	return ToolResult{Content: content}, nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/tools/ -run "TestProcessToolReadOutput" -v`
Expected: PASS

**Step 5: Commit**

```
[BEHAVIORAL] Implement ProcessTool read_output operation
```

---

### Task 16: ProcessTool — write_stdin operation

**Files:**
- Modify: `internal/tools/process.go`
- Modify: `internal/tools/process_test.go`

**Step 1: Write the failing test**

```go
func TestProcessToolWriteStdin(t *testing.T) {
	dir := t.TempDir()
	pm := NewProcessManager(dir, ProcessManagerConfig{BufferSize: 1024})
	pt := NewProcessTool(pm)
	defer pm.Shutdown(context.Background())

	// Start cat.
	execInput, _ := json.Marshal(map[string]string{
		"operation": "exec",
		"command":   "cat",
	})
	pt.Execute(context.Background(), execInput)
	procs := pm.List()
	require.Len(t, procs, 1)

	writeInput, _ := json.Marshal(map[string]string{
		"operation":  "write_stdin",
		"process_id": procs[0].ID,
		"input":      "test data\n",
	})
	result, err := pt.Execute(context.Background(), writeInput)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "sent")
}
```

**Step 2: Run test — should fail**

**Step 3: Implement**

```go
func (p *ProcessTool) writeStdinOp(in processInput) (ToolResult, error) {
	if in.ProcessID == "" {
		return ToolResult{Content: "process_id is required for write_stdin", IsError: true}, nil
	}
	if in.Input == "" {
		return ToolResult{Content: "input is required for write_stdin", IsError: true}, nil
	}

	if err := p.manager.WriteStdin(in.ProcessID, in.Input); err != nil {
		return ToolResult{Content: fmt.Sprintf("write_stdin failed: %s", err), IsError: true}, nil
	}

	return ToolResult{Content: fmt.Sprintf("sent %d bytes to process %s", len(in.Input), in.ProcessID)}, nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/tools/ -run "TestProcessToolWriteStdin" -v`
Expected: PASS

**Step 5: Commit**

```
[BEHAVIORAL] Implement ProcessTool write_stdin operation
```

---

### Task 17: ProcessTool — kill operation

**Files:**
- Modify: `internal/tools/process.go`
- Modify: `internal/tools/process_test.go`

**Step 1: Write the failing test**

```go
func TestProcessToolKill(t *testing.T) {
	dir := t.TempDir()
	pm := NewProcessManager(dir, ProcessManagerConfig{
		BufferSize:    1024,
		ShutdownGrace: 1 * time.Second,
	})
	pt := NewProcessTool(pm)
	defer pm.Shutdown(context.Background())

	execInput, _ := json.Marshal(map[string]string{
		"operation": "exec",
		"command":   "sleep 60",
	})
	pt.Execute(context.Background(), execInput)
	procs := pm.List()
	require.Len(t, procs, 1)

	killInput, _ := json.Marshal(map[string]string{
		"operation":  "kill",
		"process_id": procs[0].ID,
	})
	result, err := pt.Execute(context.Background(), killInput)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "terminated")
}
```

**Step 2: Run test — should fail**

**Step 3: Implement**

```go
func (p *ProcessTool) killOp(in processInput) (ToolResult, error) {
	if in.ProcessID == "" {
		return ToolResult{Content: "process_id is required for kill", IsError: true}, nil
	}

	if err := p.manager.Kill(in.ProcessID); err != nil {
		return ToolResult{Content: fmt.Sprintf("kill failed: %s", err), IsError: true}, nil
	}

	return ToolResult{Content: fmt.Sprintf("process %s terminated", in.ProcessID)}, nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/tools/ -run "TestProcessToolKill" -v`
Expected: PASS

**Step 5: Commit**

```
[BEHAVIORAL] Implement ProcessTool kill operation
```

---

### Task 18: ProcessTool — list operation

**Files:**
- Modify: `internal/tools/process.go`
- Modify: `internal/tools/process_test.go`

**Step 1: Write the failing test**

```go
func TestProcessToolList(t *testing.T) {
	dir := t.TempDir()
	pm := NewProcessManager(dir, ProcessManagerConfig{BufferSize: 1024})
	pt := NewProcessTool(pm)
	defer pm.Shutdown(context.Background())

	// Empty list.
	listInput, _ := json.Marshal(map[string]string{"operation": "list"})
	result, err := pt.Execute(context.Background(), listInput)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "no running processes")

	// Start one.
	execInput, _ := json.Marshal(map[string]string{
		"operation": "exec",
		"command":   "sleep 60",
	})
	pt.Execute(context.Background(), execInput)

	result, err = pt.Execute(context.Background(), listInput)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "sleep 60")
}
```

**Step 2: Run test — should fail**

**Step 3: Implement**

```go
func (p *ProcessTool) listOp() (ToolResult, error) {
	procs := p.manager.List()
	if len(procs) == 0 {
		return ToolResult{Content: "no running processes"}, nil
	}

	var content string
	for _, proc := range procs {
		content += fmt.Sprintf("- %s: %s (status: %s, started: %s)\n",
			proc.ID, proc.Command, proc.Status, proc.StartedAt.Format("15:04:05"))
	}
	return ToolResult{Content: content}, nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/tools/ -run "TestProcessToolList" -v`
Expected: PASS

**Step 5: Commit**

```
[BEHAVIORAL] Implement ProcessTool list operation
```

---

### Task 19: Update tool category for process tool

**Files:**
- Modify: `internal/tools/category.go`
- Modify existing category tests (if any)

**Step 1: Write the failing test**

Add to existing category tests or create one:

```go
func TestCategorizeProcessTool(t *testing.T) {
	assert.Equal(t, CategoryCore, Categorize("process"))
}
```

**Step 2: Run test — should fail (process maps to CategorySkill by default)**

**Step 3: Update Categorize**

```go
func Categorize(name string) ToolCategory {
	switch {
	case name == "shell" || name == "file" || name == "process":
		return CategoryCore
	// ... rest unchanged
	}
}
```

**Step 4: Run tests**

Run: `go test ./internal/tools/ -run TestCategorize -v`
Expected: PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add process tool to CategoryCore
```

---

### Task 20: Wire ProcessTool into agent registration

**Files:**
- Modify: `cmd/rubichan/main.go` (both interactive and headless registration sites)
- Modify: `internal/skills/builtin/core_tools.go`

**Step 1: Write the failing test**

Update `cmd/rubichan/helpers_test.go` if applicable, or add a test in `core_tools_test.go`:

```go
func TestCoreToolsBackendIncludesProcess(t *testing.T) {
	b := &CoreToolsBackend{WorkDir: t.TempDir()}
	err := b.Load(CoreToolsManifest(), nil)
	require.NoError(t, err)

	names := make([]string, len(b.Tools()))
	for i, tool := range b.Tools() {
		names[i] = tool.Name()
	}
	assert.Contains(t, names, "process")
}
```

**Step 2: Run test — should fail**

**Step 3: Wire registration**

In `internal/skills/builtin/core_tools.go`, update `Load`:

```go
func (b *CoreToolsBackend) Load(_ skills.SkillManifest, _ skills.PermissionChecker) error {
	pm := tools.NewProcessManager(b.WorkDir, tools.ProcessManagerConfig{})
	b.tools = []tools.Tool{
		tools.NewFileTool(b.WorkDir),
		tools.NewShellTool(b.WorkDir, defaultShellTimeout),
		tools.NewProcessTool(pm),
	}
	return nil
}
```

In `cmd/rubichan/main.go`, add `process` registration alongside `shell` and `file` in both interactive and headless paths:

```go
if shouldRegister("process", allowed) {
    pm := tools.NewProcessManager(cwd, tools.ProcessManagerConfig{})
    if err := registry.Register(tools.NewProcessTool(pm)); err != nil {
        return fmt.Errorf("registering process tool: %w", err)
    }
}
```

**Step 4: Run tests**

Run: `go test ./... -count=1`
Expected: ALL PASS

**Step 5: Commit**

```
[BEHAVIORAL] Wire ProcessTool into agent registration and core-tools
```

---

### Task 21: Run full test suite and verify coverage

**Step 1: Run all tests with coverage**

```bash
go test -cover ./internal/tools/...
```

Expected: >90% coverage for new files.

**Step 2: Run linter**

```bash
golangci-lint run ./...
```

Expected: Zero warnings.

**Step 3: Check formatting**

```bash
gofmt -l .
```

Expected: No files listed.

**Step 4: Fix any issues found**

**Step 5: Final commit if fixes needed**

```
[STRUCTURAL] Fix lint/format issues in ProcessManager implementation
```
