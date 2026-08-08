package a2a

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/julianshen/rubichan/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientDiscover(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"name": "test-agent",
			"description": "a test agent",
			"url": "` + r.Host + `",
			"skills": [{"id": "echo", "name": "Echo", "description": "echoes input"}],
			"capabilities": {"streaming": true}
		}`))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	client := NewClient(srv.URL, "", srv.Client())
	card, err := client.Discover(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "test-agent", card.Name)
	assert.Equal(t, "echo", card.Skills[0].ID)
	assert.True(t, card.Capabilities.Streaming)
}

func TestClientDiscoverNotFound(t *testing.T) {
	handler := http.NewServeMux()
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	client := NewClient(srv.URL, "", srv.Client())
	_, err := client.Discover(context.Background())
	assert.Error(t, err)
}

func TestClientDiscoverWithAPIKey(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		// Verify the Bearer token was sent
		assert.Equal(t, "Bearer test-api-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"name": "secure-agent",
			"description": "a secure agent",
			"url": "` + r.Host + `",
			"skills": [],
			"capabilities": {}
		}`))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	client := NewClient(srv.URL, "test-api-key", srv.Client())
	card, err := client.Discover(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "secure-agent", card.Name)
}

func TestClientDiscoverWithNilHTTPClient(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"name": "test-agent",
			"description": "a test agent",
			"url": "` + r.Host + `",
			"skills": [],
			"capabilities": {}
		}`))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	client := NewClient(srv.URL, "", nil)
	card, err := client.Discover(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "test-agent", card.Name)
}

func TestClientDiscoverInvalidJSON(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`invalid json`))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	client := NewClient(srv.URL, "", srv.Client())
	_, err := client.Discover(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse agent card")
}

func TestClientDiscoverCanceled(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"name": "test-agent",
			"description": "a test agent",
			"url": "` + r.Host + `",
			"skills": [],
			"capabilities": {}
		}`))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	client := NewClient(srv.URL, "", srv.Client())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Discover(ctx)
	assert.Error(t, err)
}

func TestClientSendReturnsTask(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var req RPCRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "message/send", req.Method)

		resp := RPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: mustMarshal(t, Task{
				ID: "task-1", State: TaskCompleted,
				Artifacts: []Artifact{{Name: "result", Parts: []Part{{Kind: "text", Text: "done"}}}},
			}),
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	client := NewClient(srv.URL, "", srv.Client())
	task, err := client.Send(context.Background(), Message{Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}}})
	require.NoError(t, err)

	assert.Equal(t, TaskCompleted, task.State)
	assert.Equal(t, "done", task.Artifacts[0].Parts[0].Text)
}

func TestClientSendRPCError(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var req RPCRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		resp := RPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &RPCError{Code: -32000, Message: "boom"}}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	client := NewClient(srv.URL, "", srv.Client())
	_, err := client.Send(context.Background(), Message{Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

func TestClientGetTaskAndCancelTask(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var req RPCRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		var result json.RawMessage
		switch req.Method {
		case "tasks/get":
			result = mustMarshal(t, Task{ID: "task-1", State: TaskWorking})
		case "tasks/cancel":
			result = mustMarshal(t, Task{ID: "task-1", State: TaskCanceled})
		}
		resp := RPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	client := NewClient(srv.URL, "", srv.Client())

	task, err := client.GetTask(context.Background(), "task-1")
	require.NoError(t, err)
	assert.Equal(t, TaskWorking, task.State)

	require.NoError(t, client.CancelTask(context.Background(), "task-1"))
}

func TestClientSendInvalidResponseJSON(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`invalid json`))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	client := NewClient(srv.URL, "", srv.Client())
	_, err := client.Send(context.Background(), Message{Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode response")
}

func TestClientGetTaskInvalidResponseJSON(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`invalid json`))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	client := NewClient(srv.URL, "", srv.Client())
	_, err := client.GetTask(context.Background(), "task-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode response")
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return data
}
