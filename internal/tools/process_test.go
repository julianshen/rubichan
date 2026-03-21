package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Task 13: Metadata

func TestProcessToolMetadata(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer func() { _ = pm.Shutdown(context.Background()) }()

	pt := NewProcessTool(pm)

	assert.Equal(t, "process", pt.Name())
	assert.NotEmpty(t, pt.Description())

	schema := pt.InputSchema()
	assert.NotNil(t, schema)

	// Verify schema is valid JSON.
	var schemaMap map[string]interface{}
	err := json.Unmarshal(schema, &schemaMap)
	require.NoError(t, err)

	// Verify schema has required fields.
	props, ok := schemaMap["properties"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, props, "operation")
	assert.Contains(t, props, "command")
	assert.Contains(t, props, "process_id")
	assert.Contains(t, props, "input")

	// Verify operation has enum values.
	opProp, ok := props["operation"].(map[string]interface{})
	require.True(t, ok)
	enumVals, ok := opProp["enum"].([]interface{})
	require.True(t, ok)
	assert.Len(t, enumVals, 5)
}

// Task 14: Invalid JSON and unknown operation

func TestProcessToolInvalidJSON(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer func() { _ = pm.Shutdown(context.Background()) }()

	pt := NewProcessTool(pm)

	result, err := pt.Execute(context.Background(), json.RawMessage(`{invalid`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid input")
}

func TestProcessToolUnknownOperation(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer func() { _ = pm.Shutdown(context.Background()) }()

	pt := NewProcessTool(pm)

	input, _ := json.Marshal(map[string]string{
		"operation": "bogus",
	})
	result, err := pt.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unknown operation")
	assert.Contains(t, result.Content, "bogus")
}

// Task 15: Exec

func TestProcessToolExec(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer func() { _ = pm.Shutdown(context.Background()) }()

	pt := NewProcessTool(pm)

	input, _ := json.Marshal(map[string]string{
		"operation": "exec",
		"command":   "echo hello",
	})
	result, err := pt.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "hello")
	assert.Contains(t, result.Content, "process_id")
}

func TestProcessToolExecMissingCommand(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer func() { _ = pm.Shutdown(context.Background()) }()

	pt := NewProcessTool(pm)

	input, _ := json.Marshal(map[string]string{
		"operation": "exec",
	})
	result, err := pt.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "command is required")
}

// Task 16: ReadOutput

func TestProcessToolReadOutput(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer func() { _ = pm.Shutdown(context.Background()) }()

	pt := NewProcessTool(pm)

	// Start a process first via the tool.
	execInput, _ := json.Marshal(map[string]string{
		"operation": "exec",
		"command":   "echo hello from read test",
	})
	execResult, err := pt.Execute(context.Background(), execInput)
	require.NoError(t, err)
	require.False(t, execResult.IsError)

	// Get the process ID from pm.List() rather than parsing output.
	procs := pm.List()
	require.Len(t, procs, 1)
	pid := procs[0].ID

	require.Eventually(t, func() bool {
		_, status, _ := pm.ReadOutput(pid)
		return status == ProcessExited
	}, 5*time.Second, 50*time.Millisecond)

	readInput, _ := json.Marshal(map[string]string{
		"operation":  "read_output",
		"process_id": pid,
	})
	result, err := pt.Execute(context.Background(), readInput)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "status:")
	assert.Contains(t, result.Content, "hello from read test")
}

func TestProcessToolReadOutputMissingProcessID(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer func() { _ = pm.Shutdown(context.Background()) }()

	pt := NewProcessTool(pm)

	input, _ := json.Marshal(map[string]string{
		"operation": "read_output",
	})
	result, err := pt.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "process_id is required")
}

// Task 17: WriteStdin

func TestProcessToolWriteStdin(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer func() { _ = pm.Shutdown(context.Background()) }()

	pt := NewProcessTool(pm)

	// Start cat process.
	execInput, _ := json.Marshal(map[string]string{
		"operation": "exec",
		"command":   "cat",
	})
	execResult, err := pt.Execute(context.Background(), execInput)
	require.NoError(t, err)
	require.False(t, execResult.IsError)

	procs := pm.List()
	require.Len(t, procs, 1)
	pid := procs[0].ID

	// Write to stdin.
	writeInput, _ := json.Marshal(map[string]string{
		"operation":  "write_stdin",
		"process_id": pid,
		"input":      "test data\n",
	})
	result, err := pt.Execute(context.Background(), writeInput)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "sent")
}

func TestProcessToolWriteStdinMissingFields(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer func() { _ = pm.Shutdown(context.Background()) }()

	pt := NewProcessTool(pm)

	// Missing process_id.
	input1, _ := json.Marshal(map[string]string{
		"operation": "write_stdin",
		"input":     "data",
	})
	result, err := pt.Execute(context.Background(), input1)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "process_id is required")

	// Missing input.
	input2, _ := json.Marshal(map[string]string{
		"operation":  "write_stdin",
		"process_id": "abc",
	})
	result, err = pt.Execute(context.Background(), input2)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "input is required")
}

// Task 18: Kill and List

func TestProcessToolKill(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer func() { _ = pm.Shutdown(context.Background()) }()

	pt := NewProcessTool(pm)

	// Start a long-running process.
	execInput, _ := json.Marshal(map[string]string{
		"operation": "exec",
		"command":   "sleep 60",
	})
	execResult, err := pt.Execute(context.Background(), execInput)
	require.NoError(t, err)
	require.False(t, execResult.IsError)

	procs := pm.List()
	require.Len(t, procs, 1)
	pid := procs[0].ID

	killInput, _ := json.Marshal(map[string]string{
		"operation":  "kill",
		"process_id": pid,
	})
	result, err := pt.Execute(context.Background(), killInput)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "terminated")
	assert.Contains(t, result.Content, pid)
}

func TestProcessToolKillMissingProcessID(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer func() { _ = pm.Shutdown(context.Background()) }()

	pt := NewProcessTool(pm)

	input, _ := json.Marshal(map[string]string{
		"operation": "kill",
	})
	result, err := pt.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "process_id is required")
}

func TestProcessToolList(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer func() { _ = pm.Shutdown(context.Background()) }()

	pt := NewProcessTool(pm)

	// Empty list.
	listInput, _ := json.Marshal(map[string]string{
		"operation": "list",
	})
	result, err := pt.Execute(context.Background(), listInput)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "no managed processes")

	// Start a process, then list again.
	execInput, _ := json.Marshal(map[string]string{
		"operation": "exec",
		"command":   "sleep 60",
	})
	execResult, err := pt.Execute(context.Background(), execInput)
	require.NoError(t, err)
	require.False(t, execResult.IsError)

	result, err = pt.Execute(context.Background(), listInput)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "sleep 60")
}

// Additional: manager error propagation

func TestProcessToolExecManagerError(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		MaxProcesses:  1,
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer func() { _ = pm.Shutdown(context.Background()) }()

	pt := NewProcessTool(pm)

	// Fill the process limit.
	input1, _ := json.Marshal(map[string]string{
		"operation": "exec",
		"command":   "sleep 60",
	})
	result, err := pt.Execute(context.Background(), input1)
	require.NoError(t, err)
	require.False(t, result.IsError)

	// Second exec should fail due to limit.
	input2, _ := json.Marshal(map[string]string{
		"operation": "exec",
		"command":   "echo overflow",
	})
	result, err = pt.Execute(context.Background(), input2)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "exec failed")
}

func TestProcessToolReadOutputManagerError(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer func() { _ = pm.Shutdown(context.Background()) }()

	pt := NewProcessTool(pm)

	input, _ := json.Marshal(map[string]string{
		"operation":  "read_output",
		"process_id": "nonexistent",
	})
	result, err := pt.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "read_output failed")
}

func TestProcessToolKillManagerError(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer func() { _ = pm.Shutdown(context.Background()) }()

	pt := NewProcessTool(pm)

	input, _ := json.Marshal(map[string]string{
		"operation":  "kill",
		"process_id": "nonexistent",
	})
	result, err := pt.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "kill failed")
}

func TestProcessToolWriteStdinManagerError(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer func() { _ = pm.Shutdown(context.Background()) }()

	pt := NewProcessTool(pm)

	input, _ := json.Marshal(map[string]string{
		"operation":  "write_stdin",
		"process_id": "nonexistent",
		"input":      "data",
	})
	result, err := pt.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "write_stdin failed")
}

// Verify ProcessTool satisfies Tool interface.
var _ Tool = (*ProcessTool)(nil)

// Additional: list output format shows status and ID
func TestProcessToolListFormat(t *testing.T) {
	pm := NewProcessManager(t.TempDir(), ProcessManagerConfig{
		ShutdownGrace: 500 * time.Millisecond,
	})
	defer func() { _ = pm.Shutdown(context.Background()) }()

	pt := NewProcessTool(pm)

	execInput, _ := json.Marshal(map[string]string{
		"operation": "exec",
		"command":   "sleep 60",
	})
	_, err := pt.Execute(context.Background(), execInput)
	require.NoError(t, err)

	procs := pm.List()
	require.Len(t, procs, 1)
	pid := procs[0].ID

	listInput, _ := json.Marshal(map[string]string{
		"operation": "list",
	})
	result, err := pt.Execute(context.Background(), listInput)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, pid)
	assert.True(t,
		strings.Contains(result.Content, "running") ||
			strings.Contains(result.Content, "exited") ||
			strings.Contains(result.Content, "killed"),
	)
}
