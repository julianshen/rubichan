package a2a

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientAgainstOwnServer(t *testing.T) {
	a := newTestAgent(t, "42")
	tasks := newMemoryTaskStore()
	server := NewServer(a, tasks, AgentCard{Name: "rubichan", Skills: []Skill{{ID: "chat", Name: "Chat"}}}, false, "")

	srv := testutil.NewServer(t, server.Handler())
	defer srv.Close()

	client := NewClient(srv.URL, "", srv.Client())

	card, err := client.Discover(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "rubichan", card.Name)

	task, err := client.Send(context.Background(), Message{Role: "user", Parts: []Part{{Kind: "text", Text: "what is 6*7?"}}})
	require.NoError(t, err)
	assert.Equal(t, TaskCompleted, task.State)
	assert.Contains(t, task.Artifacts[0].Parts[0].Text, "42")

	streamCh, err := client.Stream(context.Background(), Message{Role: "user", Parts: []Part{{Kind: "text", Text: "again"}}})
	require.NoError(t, err)
	var last Task
	for snapshot := range streamCh {
		last = snapshot
	}
	assert.Equal(t, TaskCompleted, last.State)
}
