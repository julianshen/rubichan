package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Task 6: Constructor and List

func TestNewProcessManager(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		MaxProcesses:  8,
		BufferSize:    1024,
		ShutdownGrace: 500 * time.Millisecond,
	})
	assert.NotNil(t, pm)
	assert.Empty(t, pm.List())
}

func TestNewProcessManagerDefaults(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{})
	assert.NotNil(t, pm)

	// Verify defaults are applied by exercising the manager.
	// We can't inspect private fields, but we can verify it works.
	assert.Empty(t, pm.List())
}

func TestProcessStatusString(t *testing.T) {
	assert.Equal(t, "running", ProcessRunning.String())
	assert.Equal(t, "exited", ProcessExited.String())
	assert.Equal(t, "killed", ProcessKilled.String())
}

// Task 7: Exec

func TestProcessManagerExec(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		MaxProcesses:  8,
		BufferSize:    4096,
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer pm.Shutdown(context.Background())

	ctx := context.Background()
	id, output, err := pm.Exec(ctx, "echo hello")
	require.NoError(t, err)
	assert.NotEmpty(t, id)
	assert.Contains(t, output, "hello")

	procs := pm.List()
	assert.Len(t, procs, 1)
	assert.Equal(t, id, procs[0].ID)
	assert.Equal(t, "echo hello", procs[0].Command)
}

// Task 8: Exec limit

func TestProcessManagerExecLimit(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		MaxProcesses:  2,
		BufferSize:    4096,
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer pm.Shutdown(context.Background())

	ctx := context.Background()

	_, _, err := pm.Exec(ctx, "sleep 60")
	require.NoError(t, err)

	_, _, err = pm.Exec(ctx, "sleep 60")
	require.NoError(t, err)

	_, _, err = pm.Exec(ctx, "sleep 60")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "process limit")
}

// Task 9: ReadOutput

func TestProcessManagerReadOutput(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		MaxProcesses:  8,
		BufferSize:    4096,
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer pm.Shutdown(context.Background())

	ctx := context.Background()
	id, _, err := pm.Exec(ctx, "echo line1; echo line2")
	require.NoError(t, err)

	// Wait for the process to exit and output to be captured.
	time.Sleep(500 * time.Millisecond)

	output, status, err := pm.ReadOutput(id)
	require.NoError(t, err)
	assert.Contains(t, output, "line1")
	assert.Contains(t, output, "line2")
	assert.Equal(t, ProcessExited, status)
}

func TestProcessManagerReadOutputUnknownID(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		MaxProcesses:  8,
		BufferSize:    4096,
		ShutdownGrace: 500 * time.Millisecond,
	})

	_, _, err := pm.ReadOutput("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// Task 10: WriteStdin

func TestProcessManagerWriteStdin(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		MaxProcesses:  8,
		BufferSize:    4096,
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer pm.Shutdown(context.Background())

	ctx := context.Background()
	id, _, err := pm.Exec(ctx, "cat")
	require.NoError(t, err)

	err = pm.WriteStdin(id, "hello from stdin\n")
	require.NoError(t, err)

	// Give cat time to echo the input back.
	time.Sleep(300 * time.Millisecond)

	output, status, err := pm.ReadOutput(id)
	require.NoError(t, err)
	assert.Contains(t, output, "hello from stdin")
	assert.Equal(t, ProcessRunning, status)
}

func TestProcessManagerWriteStdinExitedProcess(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		MaxProcesses:  8,
		BufferSize:    4096,
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer pm.Shutdown(context.Background())

	ctx := context.Background()
	id, _, err := pm.Exec(ctx, "echo done")
	require.NoError(t, err)

	// Wait for process to exit.
	time.Sleep(500 * time.Millisecond)

	err = pm.WriteStdin(id, "data")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

// Task 11: Kill

func TestProcessManagerKill(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		MaxProcesses:  8,
		BufferSize:    4096,
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer pm.Shutdown(context.Background())

	ctx := context.Background()
	id, _, err := pm.Exec(ctx, "sleep 60")
	require.NoError(t, err)

	err = pm.Kill(id)
	require.NoError(t, err)

	_, status, err := pm.ReadOutput(id)
	require.NoError(t, err)
	assert.NotEqual(t, ProcessRunning, status)
}

func TestProcessManagerKillUnknownID(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		MaxProcesses:  8,
		BufferSize:    4096,
		ShutdownGrace: 500 * time.Millisecond,
	})

	err := pm.Kill("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// Task 12: Shutdown

func TestProcessManagerShutdown(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		MaxProcesses:  8,
		BufferSize:    4096,
		ShutdownGrace: 500 * time.Millisecond,
	})

	ctx := context.Background()
	id1, _, err := pm.Exec(ctx, "sleep 60")
	require.NoError(t, err)

	id2, _, err := pm.Exec(ctx, "sleep 60")
	require.NoError(t, err)

	err = pm.Shutdown(ctx)
	require.NoError(t, err)

	// After shutdown both processes should not be running.
	procs := pm.List()
	require.Len(t, procs, 2)

	statusByID := make(map[string]ProcessStatus)
	for _, p := range procs {
		statusByID[p.ID] = p.Status
	}

	assert.NotEqual(t, ProcessRunning, statusByID[id1])
	assert.NotEqual(t, ProcessRunning, statusByID[id2])
}

// Additional edge case: Exec with cancelled context

func TestProcessManagerExecCancelledContext(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		MaxProcesses:  8,
		BufferSize:    4096,
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer pm.Shutdown(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, _, err := pm.Exec(ctx, "echo hello")
	// The process may or may not start; but the call should not panic.
	// With a cancelled context the command start may fail.
	_ = err
}

// Additional: Exec counts only running processes toward the limit

func TestProcessManagerExecLimitCountsOnlyRunning(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		MaxProcesses:  2,
		BufferSize:    4096,
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer pm.Shutdown(context.Background())

	ctx := context.Background()

	// Start a fast process that will exit.
	_, _, err := pm.Exec(ctx, "echo fast")
	require.NoError(t, err)

	// Wait for it to exit.
	time.Sleep(500 * time.Millisecond)

	// Start two long processes — both should succeed because the first exited.
	_, _, err = pm.Exec(ctx, "sleep 60")
	require.NoError(t, err)

	_, _, err = pm.Exec(ctx, "sleep 60")
	require.NoError(t, err)
}

// Additional: ID format validation

func TestProcessManagerExecIDFormat(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		MaxProcesses:  8,
		BufferSize:    4096,
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer pm.Shutdown(context.Background())

	ctx := context.Background()
	id, _, err := pm.Exec(ctx, "echo test")
	require.NoError(t, err)

	// ID should be 8 characters (uuid[:8]).
	assert.Len(t, id, 8)
	// Should contain only hex characters and hyphens (uuid format).
	for _, c := range id {
		assert.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || c == '-',
			"unexpected character %c in id %s", c, id)
	}
}

// Additional: WriteStdin unknown ID

func TestProcessManagerWriteStdinUnknownID(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		MaxProcesses:  8,
		BufferSize:    4096,
		ShutdownGrace: 500 * time.Millisecond,
	})

	err := pm.WriteStdin("nonexistent", "data")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// Additional: List returns correct ProcessInfo fields

func TestProcessManagerListFields(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		MaxProcesses:  8,
		BufferSize:    4096,
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer pm.Shutdown(context.Background())

	before := time.Now()
	ctx := context.Background()
	id, _, err := pm.Exec(ctx, "sleep 60")
	require.NoError(t, err)

	procs := pm.List()
	require.Len(t, procs, 1)

	p := procs[0]
	assert.Equal(t, id, p.ID)
	assert.Equal(t, "sleep 60", p.Command)
	assert.Equal(t, ProcessRunning, p.Status)
	assert.Equal(t, 0, p.ExitCode)
	assert.False(t, p.StartedAt.Before(before))
	assert.False(t, p.StartedAt.After(time.Now()))
}

// Additional: ReadOutput captures exit code

func TestProcessManagerReadOutputExitCode(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		MaxProcesses:  8,
		BufferSize:    4096,
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer pm.Shutdown(context.Background())

	ctx := context.Background()
	id, _, err := pm.Exec(ctx, "sh -c 'exit 42'")
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond)

	_, status, err := pm.ReadOutput(id)
	require.NoError(t, err)
	assert.Equal(t, ProcessExited, status)

	// Check the exit code via List().
	procs := pm.List()
	found := false
	for _, p := range procs {
		if p.ID == id {
			assert.Equal(t, 42, p.ExitCode)
			found = true
		}
	}
	assert.True(t, found, "process %s not found in list", id)
}

// Additional: output ring buffer wraps correctly

func TestProcessManagerOutputBufferWrap(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		MaxProcesses:  8,
		BufferSize:    32, // very small buffer
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer pm.Shutdown(context.Background())

	ctx := context.Background()
	// Generate output larger than 32 bytes.
	id, _, err := pm.Exec(ctx, "echo 'this is a long line that exceeds the buffer capacity for sure'")
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond)

	output, _, err := pm.ReadOutput(id)
	require.NoError(t, err)

	// The output should be truncated to the buffer size.
	assert.LessOrEqual(t, len(output), 32)
	// It should contain the tail end of the output.
	assert.True(t, len(output) > 0)
	// Should end with the tail of the original string.
	assert.True(t, strings.HasSuffix("this is a long line that exceeds the buffer capacity for sure\n", output),
		"expected output %q to be a suffix of the original", output)
}
