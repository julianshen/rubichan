package cmux_test

import (
	"context"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/cmux"
	"github.com/julianshen/rubichan/internal/cmux/cmuxtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOrchestrator_DispatchAndWait dispatches one task and verifies that Wait
// correctly resolves it when the log sequence delivers the completion signal.
func TestOrchestrator_DispatchAndWait(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	mc.SetResult("surface.split", cmux.Surface{ID: "surf-1", Type: "terminal"})
	mc.SetResult("surface.send_text", true)
	mc.SetResult("surface.send_key", true)
	mc.SetResult("sidebar-state", cmux.SidebarState{
		Logs: []cmux.LogEntry{
			{Message: "[DONE] task finished", Level: "info", Source: "surf-1"},
		},
	})

	orch := cmux.NewOrchestrator(mc)
	orch.SetPollRate(10 * time.Millisecond)

	task1, err := orch.Dispatch("right", "echo task1")
	require.NoError(t, err)
	assert.Equal(t, "surf-1", task1.SurfaceID)
	assert.Equal(t, "running", task1.Status)

	// Verify the correct RPC calls were made.
	calls := mc.Calls()
	require.Len(t, calls, 3)
	assert.Equal(t, "surface.split", calls[0].Method)
	assert.Equal(t, "surface.send_text", calls[1].Method)
	assert.Equal(t, "surface.send_key", calls[2].Method)

	results, err := orch.Wait(context.Background(), 5*time.Second)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "done", results[0].Status)
	assert.Equal(t, "echo task1", results[0].Command)
}

// TestOrchestrator_DispatchMultiple verifies two tasks complete with different statuses.
func TestOrchestrator_DispatchMultiple(t *testing.T) {
	mc := cmuxtest.NewMockClient()

	// MockClient returns the same result for repeated calls to the same method.
	// We need the second split to return a different ID. Use a custom approach:
	// set the result before each dispatch.
	mc.SetResult("surface.send_text", true)
	mc.SetResult("surface.send_key", true)

	// First dispatch.
	mc.SetResult("surface.split", cmux.Surface{ID: "surf-1", Type: "terminal"})
	orch := cmux.NewOrchestrator(mc)
	orch.SetPollRate(10 * time.Millisecond)

	_, err := orch.Dispatch("right", "echo task1")
	require.NoError(t, err)

	// Second dispatch — change the surface ID result.
	mc.SetResult("surface.split", cmux.Surface{ID: "surf-2", Type: "terminal"})
	_, err = orch.Dispatch("down", "echo task2")
	require.NoError(t, err)

	// Both tasks complete.
	mc.SetResult("sidebar-state", cmux.SidebarState{
		Logs: []cmux.LogEntry{
			{Message: "[DONE] task finished", Level: "info", Source: "surf-1"},
			{Message: "[ERROR] task failed", Level: "error", Source: "surf-2"},
		},
	})

	results, err := orch.Wait(context.Background(), 5*time.Second)
	require.NoError(t, err)
	require.Len(t, results, 2)

	byID := make(map[string]cmux.Task, len(results))
	for _, r := range results {
		byID[r.SurfaceID] = r
	}
	assert.Equal(t, "done", byID["surf-1"].Status)
	assert.Equal(t, "error", byID["surf-2"].Status)
}

// TestOrchestrator_Timeout verifies that Wait returns an error containing
// "timeout" when no tasks complete within the deadline.
func TestOrchestrator_Timeout(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	mc.SetResult("surface.split", cmux.Surface{ID: "surf-1", Type: "terminal"})
	mc.SetResult("surface.send_text", true)
	mc.SetResult("surface.send_key", true)
	// Empty sidebar — tasks never complete.
	mc.SetResult("sidebar-state", cmux.SidebarState{})

	orch := cmux.NewOrchestrator(mc)
	orch.SetPollRate(10 * time.Millisecond)

	_, err := orch.Dispatch("right", "sleep 999")
	require.NoError(t, err)

	_, err = orch.Wait(context.Background(), 100*time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
}

// TestOrchestrator_WaitAny returns the first completed task.
func TestOrchestrator_WaitAny(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	mc.SetResult("surface.split", cmux.Surface{ID: "surf-1", Type: "terminal"})
	mc.SetResult("surface.send_text", true)
	mc.SetResult("surface.send_key", true)
	mc.SetResult("sidebar-state", cmux.SidebarState{
		Logs: []cmux.LogEntry{
			{Message: "[DONE] first done", Level: "info", Source: "surf-1"},
		},
	})

	orch := cmux.NewOrchestrator(mc)
	orch.SetPollRate(10 * time.Millisecond)

	_, err := orch.Dispatch("right", "echo task1")
	require.NoError(t, err)

	task, err := orch.WaitAny(context.Background(), 5*time.Second)
	require.NoError(t, err)
	assert.Equal(t, "done", task.Status)
}

// TestOrchestrator_DispatchError verifies Dispatch handles split failures.
func TestOrchestrator_DispatchError(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	// Set a result that cannot be decoded as Surface.
	mc.SetResult("surface.split", "not-a-surface")

	orch := cmux.NewOrchestrator(mc)
	_, err := orch.Dispatch("right", "echo hello")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "orchestrator")
}

// TestOrchestrator_DispatchSplitOKFalse verifies Dispatch handles split returning OK:false.
func TestOrchestrator_DispatchSplitOKFalse(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	mc.SetError("surface.split", "no space left")

	orch := cmux.NewOrchestrator(mc)
	_, err := orch.Dispatch("right", "echo hello")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "split")
	assert.Contains(t, err.Error(), "no space left")
}

// TestOrchestrator_DispatchSendTextOKFalse verifies Dispatch handles send_text OK:false.
func TestOrchestrator_DispatchSendTextOKFalse(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	mc.SetResult("surface.split", cmux.Surface{ID: "surf-1", Type: "terminal"})
	mc.SetError("surface.send_text", "surface not found")
	mc.SetResult("surface.send_key", true)

	orch := cmux.NewOrchestrator(mc)
	_, err := orch.Dispatch("right", "echo hello")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "send_text")
	assert.Contains(t, err.Error(), "surface not found")
}

// TestOrchestrator_DispatchSendKeyOKFalse verifies Dispatch handles send_key OK:false.
func TestOrchestrator_DispatchSendKeyOKFalse(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	mc.SetResult("surface.split", cmux.Surface{ID: "surf-1", Type: "terminal"})
	mc.SetResult("surface.send_text", true)
	mc.SetError("surface.send_key", "key rejected")

	orch := cmux.NewOrchestrator(mc)
	_, err := orch.Dispatch("right", "echo hello")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "send_key")
	assert.Contains(t, err.Error(), "key rejected")
}

// TestOrchestrator_WaitAnyTimeout verifies WaitAny returns a timeout error.
func TestOrchestrator_WaitAnyTimeout(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	mc.SetResult("surface.split", cmux.Surface{ID: "surf-1", Type: "terminal"})
	mc.SetResult("surface.send_text", true)
	mc.SetResult("surface.send_key", true)
	mc.SetResult("sidebar-state", cmux.SidebarState{})

	orch := cmux.NewOrchestrator(mc)
	orch.SetPollRate(10 * time.Millisecond)

	_, err := orch.Dispatch("right", "sleep 999")
	require.NoError(t, err)

	_, err = orch.WaitAny(context.Background(), 100*time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
}

// TestOrchestrator_WaitContextCancelled verifies Wait returns on context cancellation.
func TestOrchestrator_WaitContextCancelled(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	mc.SetResult("surface.split", cmux.Surface{ID: "surf-1", Type: "terminal"})
	mc.SetResult("surface.send_text", true)
	mc.SetResult("surface.send_key", true)
	mc.SetResult("sidebar-state", cmux.SidebarState{})

	orch := cmux.NewOrchestrator(mc)
	orch.SetPollRate(10 * time.Millisecond)

	_, err := orch.Dispatch("right", "sleep 999")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err = orch.Wait(ctx, 5*time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "canceled")
}

// TestOrchestrator_PollOKFalse verifies Wait returns an error when poll gets OK:false.
func TestOrchestrator_PollOKFalse(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	mc.SetResult("surface.split", cmux.Surface{ID: "surf-1", Type: "terminal"})
	mc.SetResult("surface.send_text", true)
	mc.SetResult("surface.send_key", true)
	mc.SetError("sidebar-state", "sidebar unavailable")

	orch := cmux.NewOrchestrator(mc)
	orch.SetPollRate(10 * time.Millisecond)

	_, err := orch.Dispatch("right", "echo task1")
	require.NoError(t, err)

	_, err = orch.Wait(context.Background(), 5*time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "poll")
	assert.Contains(t, err.Error(), "sidebar unavailable")
}
