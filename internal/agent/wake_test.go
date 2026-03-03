package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWakeManagerSubmitAndComplete(t *testing.T) {
	wm := NewWakeManager()
	_, cancel := context.WithCancel(context.Background())
	taskID := wm.Submit("explorer", cancel)
	assert.NotEmpty(t, taskID)

	wm.Complete(taskID, &SubagentResult{Name: "explorer", Output: "found files"})

	select {
	case event := <-wm.Events():
		assert.Equal(t, "explorer", event.AgentName)
		assert.Equal(t, taskID, event.TaskID)
		assert.Equal(t, "found files", event.Result.Output)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for wake event")
	}
}

func TestWakeManagerStatus(t *testing.T) {
	wm := NewWakeManager()
	_, cancel1 := context.WithCancel(context.Background())
	id1 := wm.Submit("agent1", cancel1)
	_, cancel2 := context.WithCancel(context.Background())
	id2 := wm.Submit("agent2", cancel2)

	statuses := wm.Status()
	assert.Len(t, statuses, 2)

	wm.Complete(id1, &SubagentResult{Name: "agent1", Output: "done"})
	<-wm.Events()

	statuses = wm.Status()
	assert.Len(t, statuses, 1)
	assert.Equal(t, id2, statuses[0].ID)
	assert.Equal(t, "running", statuses[0].Status)
}

func TestWakeManagerPendingCount(t *testing.T) {
	wm := NewWakeManager()
	assert.Equal(t, 0, wm.PendingCount())
	_, cancel := context.WithCancel(context.Background())
	wm.Submit("test", cancel)
	assert.Equal(t, 1, wm.PendingCount())
}

func TestWakeManagerCompleteUnknownID(t *testing.T) {
	wm := NewWakeManager()
	// Should not panic or block.
	wm.Complete("nonexistent", &SubagentResult{})
	assert.Equal(t, 0, wm.PendingCount())
}
