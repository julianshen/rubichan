package cmux

import (
	"fmt"
	"strings"
	"time"
)

// Task represents a sub-agent command dispatched to a cmux surface.
type Task struct {
	ID        string
	SurfaceID string
	Command   string
	Status    string     // "running", "done", "error"
	Logs      []LogEntry // accumulated log entries from sidebar
}

// Orchestrator coordinates sub-agents across split panes using log-based signaling.
type Orchestrator struct {
	client   *Client
	tasks    map[string]*Task // surface ID → task
	pollRate time.Duration    // default 2s
}

// NewOrchestrator creates a new Orchestrator backed by the given Client.
func NewOrchestrator(client *Client) *Orchestrator {
	return &Orchestrator{
		client:   client,
		tasks:    make(map[string]*Task),
		pollRate: 2 * time.Second,
	}
}

// SetPollRate sets the polling interval used by Wait and WaitAny.
func (o *Orchestrator) SetPollRate(d time.Duration) {
	o.pollRate = d
}

// Dispatch splits a new pane in the given direction, sends the command to it,
// and registers a Task with Status "running". The task is stored keyed by
// the new surface's ID.
func (o *Orchestrator) Dispatch(direction, command string) (*Task, error) {
	surf, err := o.client.Split(direction)
	if err != nil {
		return nil, fmt.Errorf("cmux: orchestrator: split: %w", err)
	}

	if err := o.client.SendText(surf.ID, command); err != nil {
		return nil, fmt.Errorf("cmux: orchestrator: send_text: %w", err)
	}

	if err := o.client.SendKey(surf.ID, "enter"); err != nil {
		return nil, fmt.Errorf("cmux: orchestrator: send_key: %w", err)
	}

	task := &Task{
		ID:        surf.ID,
		SurfaceID: surf.ID,
		Command:   command,
		Status:    "running",
	}
	o.tasks[surf.ID] = task
	return task, nil
}

// Wait polls SidebarState every pollRate and updates task statuses based on
// log entries whose Source matches a task's SurfaceID. A log entry with a
// "[DONE]" prefix marks the task done; "[ERROR]" marks it errored. All entries
// are appended to the matching task's Logs.
//
// Wait returns when all tasks have finished (status != "running"), or returns
// an error if timeout elapses first.
func (o *Orchestrator) Wait(timeout time.Duration) ([]Task, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if err := o.poll(); err != nil {
			return nil, err
		}
		if o.allDone() {
			return o.snapshot(), nil
		}
		remaining := time.Until(deadline)
		sleep := o.pollRate
		if sleep > remaining {
			sleep = remaining
		}
		time.Sleep(sleep)
	}

	// Final check after timeout.
	if o.allDone() {
		return o.snapshot(), nil
	}
	return nil, fmt.Errorf("cmux: orchestrator: timeout waiting for tasks")
}

// WaitAny polls SidebarState every pollRate and returns as soon as any task
// completes (status != "running"). Returns an error if timeout elapses before
// any task completes.
func (o *Orchestrator) WaitAny(timeout time.Duration) (*Task, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if err := o.poll(); err != nil {
			return nil, err
		}
		if t := o.firstDone(); t != nil {
			return t, nil
		}
		remaining := time.Until(deadline)
		sleep := o.pollRate
		if sleep > remaining {
			sleep = remaining
		}
		time.Sleep(sleep)
	}

	// Final check after timeout.
	if t := o.firstDone(); t != nil {
		return t, nil
	}
	return nil, fmt.Errorf("cmux: orchestrator: timeout waiting for tasks")
}

// poll fetches sidebar state and updates all running tasks from log entries.
func (o *Orchestrator) poll() error {
	state, err := o.client.SidebarState()
	if err != nil {
		return fmt.Errorf("cmux: orchestrator: poll: %w", err)
	}

	for _, entry := range state.Logs {
		task, ok := o.tasks[entry.Source]
		if !ok {
			continue
		}
		task.Logs = append(task.Logs, entry)
		if strings.HasPrefix(entry.Message, "[DONE]") {
			task.Status = "done"
		} else if strings.HasPrefix(entry.Message, "[ERROR]") {
			task.Status = "error"
		}
	}
	return nil
}

// allDone returns true when every task has a non-"running" status.
func (o *Orchestrator) allDone() bool {
	for _, t := range o.tasks {
		if t.Status == "running" {
			return false
		}
	}
	return true
}

// firstDone returns the first task that is no longer running, or nil.
func (o *Orchestrator) firstDone() *Task {
	for _, t := range o.tasks {
		if t.Status != "running" {
			return t
		}
	}
	return nil
}

// snapshot returns a copy of all tasks as a slice.
func (o *Orchestrator) snapshot() []Task {
	result := make([]Task, 0, len(o.tasks))
	for _, t := range o.tasks {
		result = append(result, *t)
	}
	return result
}
