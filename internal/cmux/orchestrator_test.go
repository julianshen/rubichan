package cmux_test

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/cmux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeOrchestratorServer handles split + send_text + send_key + sidebar-state
// with a logSequence that returns different logs on each sidebar-state poll.
//
// Methods handled:
//   - system.ping → {"pong": true}
//   - system.identify → standard identity
//   - surface.split → incrementing surface IDs ("surf-1", "surf-2", …)
//   - surface.send_text, surface.send_key → true
//   - sidebar-state → returns {"logs": logSequence[pollCount]} cycling through the sequence
func fakeOrchestratorServer(t *testing.T, ln net.Listener, logSequence [][]cmux.LogEntry) {
	t.Helper()

	var splitCounter atomic.Int64
	var pollCounter atomic.Int64

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			go serveConn(t, conn, map[string]handlerFunc{
				"system.ping": func(req jsonrpcRequest) interface{} {
					return map[string]bool{"pong": true}
				},
				"system.identify": func(req jsonrpcRequest) interface{} {
					return map[string]string{
						"window_id":    "win-1",
						"workspace_id": "ws-1",
						"pane_id":      "pane-1",
						"surface_id":   "surf-0",
					}
				},
				"surface.split": func(req jsonrpcRequest) interface{} {
					n := splitCounter.Add(1)
					return map[string]string{
						"id":   fmt.Sprintf("surf-%d", n),
						"type": "terminal",
					}
				},
				"surface.send_text": func(req jsonrpcRequest) interface{} {
					return true
				},
				"surface.send_key": func(req jsonrpcRequest) interface{} {
					return true
				},
				"sidebar-state": func(req jsonrpcRequest) interface{} {
					idx := int(pollCounter.Add(1)) - 1
					if idx >= len(logSequence) {
						idx = len(logSequence) - 1
					}
					return map[string]interface{}{
						"logs": logSequence[idx],
					}
				},
			})
		}
	}()
}

// newOrchestratorTestServer starts a fakeOrchestratorServer and returns
// a dialled client registered for cleanup.
// Uses os.TempDir() + short name to stay within the 104-char macOS socket-path limit.
func newOrchestratorTestServer(t *testing.T, logSequence [][]cmux.LogEntry) *cmux.Client {
	t.Helper()
	socketPath := filepath.Join(os.TempDir(), fmt.Sprintf("co_%d_%s.sock", os.Getpid(), t.Name()[:min(len(t.Name()), 10)]))
	_ = os.Remove(socketPath)
	t.Cleanup(func() { os.Remove(socketPath) })
	ln, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	fakeOrchestratorServer(t, ln, logSequence)

	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })
	return c
}

// TestOrchestrator_DispatchAndWait dispatches two tasks and verifies that Wait
// correctly resolves both tasks when the log sequence delivers their signals.
//
// logSequence:
//   - poll 1: empty
//   - poll 2: surf-1 DONE
//   - poll 3: surf-1 DONE + surf-2 ERROR
func TestOrchestrator_DispatchAndWait(t *testing.T) {
	seq := [][]cmux.LogEntry{
		// poll 1: empty
		{},
		// poll 2: surf-1 done
		{
			{Message: "[DONE] task finished", Level: "info", Source: "surf-1"},
		},
		// poll 3: surf-1 done + surf-2 error
		{
			{Message: "[DONE] task finished", Level: "info", Source: "surf-1"},
			{Message: "[ERROR] task failed", Level: "error", Source: "surf-2"},
		},
	}

	client := newOrchestratorTestServer(t, seq)
	orch := cmux.NewOrchestrator(client)
	orch.SetPollRate(50 * time.Millisecond)

	task1, err := orch.Dispatch("right", "echo task1")
	require.NoError(t, err)
	assert.Equal(t, "surf-1", task1.SurfaceID)

	task2, err := orch.Dispatch("down", "echo task2")
	require.NoError(t, err)
	assert.Equal(t, "surf-2", task2.SurfaceID)

	results, err := orch.Wait(5 * time.Second)
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
	// Always empty — tasks never complete.
	seq := [][]cmux.LogEntry{
		{},
	}

	client := newOrchestratorTestServer(t, seq)
	orch := cmux.NewOrchestrator(client)
	orch.SetPollRate(50 * time.Millisecond)

	_, err := orch.Dispatch("right", "sleep 999")
	require.NoError(t, err)

	_, err = orch.Wait(200 * time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
}
