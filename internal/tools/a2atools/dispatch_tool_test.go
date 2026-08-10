package a2atools

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/julianshen/rubichan/internal/a2a"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDispatchToolCallsBestMatch(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var req a2a.RPCRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		result, _ := json.Marshal(a2a.Task{
			ID: "task-1", State: a2a.TaskCompleted,
			Artifacts: []a2a.Artifact{{Parts: []a2a.Part{{Kind: "text", Text: "sunny"}}}},
		})
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(a2a.RPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result}))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.A2A.Agents = []config.A2AAgentConfig{{Name: "weather-bot", URL: srv.URL}}

	reg := a2a.NewRegistry()
	reg.Add("weather-bot", a2a.AgentCard{
		Name:   "weather-bot",
		Skills: []a2a.Skill{{ID: "forecast", Tags: []string{"weather"}}},
	})

	tool := NewDispatchTool(cfg, reg)
	tool.httpClient = srv.Client()

	input, err := json.Marshal(map[string]string{"task_description": "what's the weather like"})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "sunny")
}

func TestDispatchToolNoMatch(t *testing.T) {
	cfg := config.DefaultConfig()
	reg := a2a.NewRegistry()
	tool := NewDispatchTool(cfg, reg)

	input, err := json.Marshal(map[string]string{"task_description": "anything"})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "no known agent matches")
}

func TestDispatchToolInvalidInput(t *testing.T) {
	cfg := config.DefaultConfig()
	reg := a2a.NewRegistry()
	tool := NewDispatchTool(cfg, reg)

	result, err := tool.Execute(context.Background(), json.RawMessage(`invalid json`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid input")
}

func TestDispatchToolResolveAgentURLFailure(t *testing.T) {
	cfg := config.DefaultConfig()
	reg := a2a.NewRegistry()
	// Add agent without configuration
	reg.Add("unknown-bot", a2a.AgentCard{
		Name:   "unknown-bot",
		Skills: []a2a.Skill{{ID: "skill1", Tags: []string{"unknown"}}},
	})

	tool := NewDispatchTool(cfg, reg)

	input, err := json.Marshal(map[string]string{"task_description": "unknown"})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "not a configured")
}

func TestDispatchToolTaskNotCompleted(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var req a2a.RPCRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		result, _ := json.Marshal(a2a.Task{
			ID: "task-2", State: a2a.TaskFailed,
			Error: "something went wrong",
		})
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(a2a.RPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result}))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.A2A.Agents = []config.A2AAgentConfig{{Name: "failing-bot", URL: srv.URL}}

	reg := a2a.NewRegistry()
	reg.Add("failing-bot", a2a.AgentCard{
		Name:   "failing-bot",
		Skills: []a2a.Skill{{ID: "test", Tags: []string{"fail"}}},
	})

	tool := NewDispatchTool(cfg, reg)
	tool.httpClient = srv.Client()

	input, err := json.Marshal(map[string]string{"task_description": "fail me"})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "failed")
	assert.Contains(t, result.Content, "something went wrong")
}
