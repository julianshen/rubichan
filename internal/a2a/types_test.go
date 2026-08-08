package a2a

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskJSONRoundTrip(t *testing.T) {
	task := Task{
		ID:        "task-1",
		ContextID: "ctx-1",
		State:     TaskCompleted,
		Messages: []Message{
			{Role: "agent", Parts: []Part{{Kind: "text", Text: "hello"}}},
		},
		Artifacts: []Artifact{
			{Name: "result", Parts: []Part{{Kind: "text", Text: "42"}}},
		},
	}

	data, err := json.Marshal(task)
	require.NoError(t, err)

	var decoded Task
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, task, decoded)
	assert.Contains(t, string(data), `"state":"completed"`)
}

func TestTaskStateConstants(t *testing.T) {
	assert.Equal(t, TaskState("submitted"), TaskSubmitted)
	assert.Equal(t, TaskState("working"), TaskWorking)
	assert.Equal(t, TaskState("input-required"), TaskInputRequired)
	assert.Equal(t, TaskState("auth-required"), TaskAuthRequired)
	assert.Equal(t, TaskState("completed"), TaskCompleted)
	assert.Equal(t, TaskState("failed"), TaskFailed)
	assert.Equal(t, TaskState("canceled"), TaskCanceled)
	assert.Equal(t, TaskState("rejected"), TaskRejected)
}
