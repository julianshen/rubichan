package agent

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

// WakeEvent signals that a background subagent has completed.
type WakeEvent struct {
	AgentName string
	TaskID    string
	Result    *SubagentResult
}

// TaskStatus represents the current state of a background task.
type TaskStatus struct {
	ID        string
	AgentName string
	Status    string // "running"
}

// backgroundTask tracks a running subagent goroutine.
type backgroundTask struct {
	id        string
	agentName string
	cancel    context.CancelFunc
}

// WakeManager tracks background subagent tasks and delivers completion events.
type WakeManager struct {
	mu      sync.Mutex
	pending map[string]*backgroundTask
	wakeCh  chan WakeEvent
}

// NewWakeManager creates a WakeManager with a buffered event channel.
func NewWakeManager() *WakeManager {
	return &WakeManager{
		pending: make(map[string]*backgroundTask),
		wakeCh:  make(chan WakeEvent, 16),
	}
}

// Submit registers a new background task and returns its unique ID.
func (wm *WakeManager) Submit(name string, cancel context.CancelFunc) string {
	id := uuid.New().String()[:8]
	wm.mu.Lock()
	defer wm.mu.Unlock()
	wm.pending[id] = &backgroundTask{
		id:        id,
		agentName: name,
		cancel:    cancel,
	}
	return id
}

// Complete marks a background task as done and sends a wake event.
func (wm *WakeManager) Complete(taskID string, result *SubagentResult) {
	wm.mu.Lock()
	task, ok := wm.pending[taskID]
	if ok {
		delete(wm.pending, taskID)
	}
	wm.mu.Unlock()
	if ok {
		wm.wakeCh <- WakeEvent{
			AgentName: task.agentName,
			TaskID:    taskID,
			Result:    result,
		}
	}
}

// Events returns the channel for receiving wake events.
func (wm *WakeManager) Events() <-chan WakeEvent {
	return wm.wakeCh
}

// Status returns the current status of all pending tasks.
func (wm *WakeManager) Status() []TaskStatus {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	result := make([]TaskStatus, 0, len(wm.pending))
	for _, task := range wm.pending {
		result = append(result, TaskStatus{
			ID:        task.id,
			AgentName: task.agentName,
			Status:    "running",
		})
	}
	return result
}

// PendingCount returns the number of background tasks still running.
func (wm *WakeManager) PendingCount() int {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	return len(wm.pending)
}
