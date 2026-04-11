package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/cmux"
	"github.com/julianshen/rubichan/internal/cmux/cmuxtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCmuxOrchestrateTool_Name(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	tool := NewCmuxOrchestrate(mc)
	assert.Equal(t, "cmux_orchestrate", tool.Name())
}

func TestCmuxOrchestrateTool_Execute_SingleTask(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	mc.SetResult("surface.split", cmux.Surface{ID: "orch-surf-1", Type: "terminal"})
	mc.SetResult("surface.send_text", true)
	mc.SetResult("surface.send_key", true)
	mc.SetResult("sidebar-state", cmux.SidebarState{
		Logs: []cmux.LogEntry{
			{Message: "[DONE] finished", Level: "info", Source: "orch-surf-1"},
		},
	})

	tool := NewCmuxOrchestrate(mc)
	input, _ := json.Marshal(orchestrateInput{
		Tasks: []orchestrateTask{
			{Direction: "right", Command: "echo hello"},
		},
		Timeout: "10s",
	})
	result, err := tool.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.False(t, result.IsError, result.Content)
	assert.Contains(t, result.Content, "orch-surf-1")
	assert.Contains(t, result.Content, "done")
}

func TestCmuxOrchestrateTool_Execute_EmptyTasks(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	tool := NewCmuxOrchestrate(mc)
	input, _ := json.Marshal(orchestrateInput{Tasks: []orchestrateTask{}})
	result, err := tool.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "tasks must not be empty")
}

func TestCmuxOrchestrateTool_Execute_InvalidTimeout(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	tool := NewCmuxOrchestrate(mc)
	input, _ := json.Marshal(orchestrateInput{
		Tasks:   []orchestrateTask{{Direction: "right", Command: "echo hi"}},
		Timeout: "notaduration",
	})
	result, err := tool.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid timeout")
}

func TestCmuxOrchestrateTool_Execute_ErrorLog(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	mc.SetResult("surface.split", cmux.Surface{ID: "orch-surf-2", Type: "terminal"})
	mc.SetResult("surface.send_text", true)
	mc.SetResult("surface.send_key", true)
	mc.SetResult("sidebar-state", cmux.SidebarState{
		Logs: []cmux.LogEntry{
			{Message: "[ERROR] crashed", Level: "error", Source: "orch-surf-2"},
		},
	})

	tool := NewCmuxOrchestrate(mc)
	input, _ := json.Marshal(orchestrateInput{
		Tasks:   []orchestrateTask{{Direction: "down", Command: "bad-cmd"}},
		Timeout: "10s",
	})
	result, err := tool.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "error")
}
