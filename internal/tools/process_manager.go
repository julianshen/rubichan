package tools

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
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
	cmd       *exec.Cmd
	io        ProcessIO
	output    *RingBuffer
	status    ProcessStatus
	exitCode  int
	startedAt time.Time
	mu        sync.Mutex
	done      chan struct{}
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

// Exec starts a new process and returns its ID and initial output.
// It waits briefly (up to 1 second) to capture startup output.
func (pm *ProcessManager) Exec(ctx context.Context, command string) (string, string, error) {
	pm.mu.Lock()
	running := 0
	for _, p := range pm.processes {
		p.mu.Lock()
		if p.status == ProcessRunning {
			running++
		}
		p.mu.Unlock()
	}
	if running >= pm.maxProcesses {
		pm.mu.Unlock()
		return "", "", fmt.Errorf("process limit reached (%d)", pm.maxProcesses)
	}

	// Generate a unique ID while holding the lock to prevent races.
	id := uuid.New().String()[:8]
	for pm.processes[id] != nil {
		id = uuid.New().String()[:8]
	}

	// Reserve the slot with a placeholder so concurrent Exec calls
	// see this slot as occupied during the limit check above.
	pm.processes[id] = &managedProcess{
		id:     id,
		status: ProcessRunning,
	}
	pm.mu.Unlock()

	// Use exec.Command (not CommandContext) so that parent context
	// cancellation does not bypass our graceful SIGTERM shutdown path.
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = pm.workDir

	pio, err := NewPipeProcessIO(cmd)
	if err != nil {
		pm.mu.Lock()
		delete(pm.processes, id)
		pm.mu.Unlock()
		return "", "", fmt.Errorf("creating process I/O: %w", err)
	}

	if err := cmd.Start(); err != nil {
		pio.Close()
		pm.mu.Lock()
		delete(pm.processes, id)
		pm.mu.Unlock()
		return "", "", fmt.Errorf("starting process: %w", err)
	}

	proc := &managedProcess{
		id:        id,
		command:   command,
		cmd:       cmd,
		io:        pio,
		output:    NewRingBuffer(pm.bufferSize),
		status:    ProcessRunning,
		startedAt: time.Now(),
		done:      make(chan struct{}),
	}

	pm.mu.Lock()
	pm.processes[id] = proc
	pm.mu.Unlock()

	go pm.readLoop(proc)
	go pm.waitLoop(proc)

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
func (pm *ProcessManager) waitLoop(proc *managedProcess) {
	err := proc.cmd.Wait()

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

// Shutdown terminates all running processes with a grace period.
// The context can be used to impose a deadline on the entire shutdown.
func (pm *ProcessManager) Shutdown(ctx context.Context) error {
	pm.mu.Lock()
	procs := make([]*managedProcess, 0, len(pm.processes))
	for _, p := range pm.processes {
		procs = append(procs, p)
	}
	pm.mu.Unlock()

	for _, p := range procs {
		pm.killProcess(p, ctx)
	}
	return nil
}

// killProcess sends SIGTERM, waits for the grace period, then falls back
// to SIGKILL if the process has not exited. An optional context can
// shorten the grace period.
func (pm *ProcessManager) killProcess(proc *managedProcess, ctxs ...context.Context) {
	proc.mu.Lock()
	if proc.status != ProcessRunning {
		proc.mu.Unlock()
		return
	}
	proc.mu.Unlock()

	// Send SIGTERM for graceful shutdown.
	if proc.cmd.Process != nil {
		_ = proc.cmd.Process.Signal(syscall.SIGTERM)
	}

	graceTimer := time.After(pm.shutdownGrace)

	// Build a context done channel (nil if no context supplied).
	var ctxDone <-chan struct{}
	if len(ctxs) > 0 && ctxs[0] != nil {
		ctxDone = ctxs[0].Done()
	}

	select {
	case <-proc.done:
		return // exited cleanly after SIGTERM
	case <-graceTimer:
	case <-ctxDone:
	}

	// Close I/O and force kill if still running.
	proc.io.Close()
	if proc.cmd.Process != nil {
		_ = proc.cmd.Process.Kill()
	}

	proc.mu.Lock()
	if proc.status == ProcessRunning {
		proc.status = ProcessKilled
	}
	proc.mu.Unlock()

	// Wait for the done channel to close after force kill.
	<-proc.done
}
