package a2a

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreTaskStoreCreateGetUpdate(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	ts := NewStoreTaskStore(s, "client")
	ctx := context.Background()

	task := Task{ID: "task-1", ContextID: "ctx-1", State: TaskSubmitted}
	require.NoError(t, ts.Create(ctx, task))

	got, err := ts.Get(ctx, "task-1")
	require.NoError(t, err)
	assert.Equal(t, task, got)

	task.State = TaskCompleted
	require.NoError(t, ts.Update(ctx, task))

	got, err = ts.Get(ctx, "task-1")
	require.NoError(t, err)
	assert.Equal(t, TaskCompleted, got.State)
}

func TestStoreTaskStoreGetNotFound(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	ts := NewStoreTaskStore(s, "client")
	ctx := context.Background()

	// Attempt to get a task that was never created.
	_, err = ts.Get(ctx, "nonexistent-id")
	require.Error(t, err)
}

func TestStoreTaskStoreUpdateNotFound(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	ts := NewStoreTaskStore(s, "client")
	ctx := context.Background()

	// Attempt to update a task that was never created.
	task := Task{ID: "nonexistent-id", ContextID: "ctx-1", State: TaskCompleted}
	err = ts.Update(ctx, task)
	require.Error(t, err)
}
