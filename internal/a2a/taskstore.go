package a2a

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/julianshen/rubichan/internal/store"
)

// TaskStore persists Task state so a crash mid-poll or mid-execution is
// recoverable rather than silently forgetten. Server and client sides
// each own their own TaskStore instance (see NewStoreTaskStore's role
// parameter).
type TaskStore interface {
	Create(ctx context.Context, t Task) error
	Get(ctx context.Context, id string) (Task, error)
	Update(ctx context.Context, t Task) error
}

// storeTaskStore implements TaskStore over the shared internal/store.Store
// database, rather than opening a second SQLite connection. role is
// "server" or "client", keeping the two sides' task IDs from colliding
// in the shared a2a_tasks table.
type storeTaskStore struct {
	store *store.Store
	role  string
}

// NewStoreTaskStore creates a TaskStore backed by s, scoped to role.
func NewStoreTaskStore(s *store.Store, role string) TaskStore {
	return &storeTaskStore{store: s, role: role}
}

func (t *storeTaskStore) Create(_ context.Context, task Task) error {
	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshal task: %w", err)
	}
	return t.store.CreateA2ATask(t.role, task.ID, task.ContextID, string(task.State), string(data))
}

func (t *storeTaskStore) Get(_ context.Context, id string) (Task, error) {
	data, _, err := t.store.GetA2ATask(t.role, id)
	if err != nil {
		return Task{}, err
	}
	var task Task
	if err := json.Unmarshal([]byte(data), &task); err != nil {
		return Task{}, fmt.Errorf("unmarshal task: %w", err)
	}
	return task, nil
}

func (t *storeTaskStore) Update(_ context.Context, task Task) error {
	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshal task: %w", err)
	}
	return t.store.UpdateA2ATask(t.role, task.ID, string(task.State), string(data))
}
