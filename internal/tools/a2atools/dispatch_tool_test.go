package a2atools

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

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

// TestDispatchToolRejectsPrivateAddress guards against the SSRF finding:
// a2a_dispatch resolves the matched agent's configured URL through the same
// resolveAgentURL/httpClient path as a2a_discover and a2a_call, so a
// misconfigured (or attacker-influenced) [[a2a.agents]] entry pointing at an
// internal address must be blocked too — the check applies uniformly, with
// no allowlist exemption for configured agents, matching internal/tools/http.
// httpClient is left nil to exercise the production default.
func TestDispatchToolRejectsPrivateAddress(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.A2A.Agents = []config.A2AAgentConfig{{Name: "internal-bot", URL: "http://127.0.0.1:1/"}}

	reg := a2a.NewRegistry()
	reg.Add("internal-bot", a2a.AgentCard{
		Name:   "internal-bot",
		Skills: []a2a.Skill{{ID: "probe", Tags: []string{"probe"}}},
	})

	tool := NewDispatchTool(cfg, reg)

	input, err := json.Marshal(map[string]string{"task_description": "probe"})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "private or local addresses")
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

// TestDispatchToolFanout is a regression test for the finding that
// a2a_dispatch dropped the design spec's fanout parameter: with fanout=true,
// every agent Registry.Match returns (not just the best match) should be
// called, and all their results should appear in the output, each prefixed
// with its own agent name.
func TestDispatchToolFanout(t *testing.T) {
	weatherHandler := http.NewServeMux()
	weatherHandler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPCRequest(t, r)
		writeRPCResult(t, w, req, a2a.Task{
			ID: "task-weather", State: a2a.TaskCompleted,
			Artifacts: []a2a.Artifact{{Parts: []a2a.Part{{Kind: "text", Text: "sunny"}}}},
		})
	})
	weatherSrv := testutil.NewServer(t, weatherHandler)
	defer weatherSrv.Close()

	newsHandler := http.NewServeMux()
	newsHandler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPCRequest(t, r)
		writeRPCResult(t, w, req, a2a.Task{
			ID: "task-news", State: a2a.TaskCompleted,
			Artifacts: []a2a.Artifact{{Parts: []a2a.Part{{Kind: "text", Text: "breaking news"}}}},
		})
	})
	newsSrv := testutil.NewServer(t, newsHandler)
	defer newsSrv.Close()

	cfg := config.DefaultConfig()
	cfg.A2A.Agents = []config.A2AAgentConfig{
		{Name: "weather-bot", URL: weatherSrv.URL},
		{Name: "news-bot", URL: newsSrv.URL},
	}

	reg := a2a.NewRegistry()
	reg.Add("weather-bot", a2a.AgentCard{
		Name:   "weather-bot",
		Skills: []a2a.Skill{{ID: "forecast", Tags: []string{"today"}}},
	})
	reg.Add("news-bot", a2a.AgentCard{
		Name:   "news-bot",
		Skills: []a2a.Skill{{ID: "headlines", Tags: []string{"today"}}},
	})

	tool := NewDispatchTool(cfg, reg)

	input, err := json.Marshal(map[string]any{"task_description": "what's happening today", "fanout": true})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "[weather-bot]")
	assert.Contains(t, result.Content, "sunny")
	assert.Contains(t, result.Content, "[news-bot]")
	assert.Contains(t, result.Content, "breaking news")
}

// TestDispatchToolPollsUntilCompleted is a regression test for the finding
// that DispatchTool.Execute treated any non-TaskCompleted state returned by
// Send as a hard failure, even though Send's own contract allows returning
// a task still in submitted/working state for a genuinely asynchronous
// remote agent (not every A2A agent responds synchronously the way
// rubichan's own server currently does). Mirrors CallTool's
// TestCallToolPollLoop: message/send returns "working", and two tasks/get
// polls are required before the task reaches TaskCompleted.
func TestDispatchToolPollsUntilCompleted(t *testing.T) {
	var getTaskCalls atomic.Int32

	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPCRequest(t, r)
		switch req.Method {
		case "message/send":
			writeRPCResult(t, w, req, a2a.Task{ID: "task-dispatch-poll", State: a2a.TaskWorking})
		case "tasks/get":
			n := getTaskCalls.Add(1)
			if n < 2 {
				writeRPCResult(t, w, req, a2a.Task{ID: "task-dispatch-poll", State: a2a.TaskWorking})
				return
			}
			writeRPCResult(t, w, req, a2a.Task{
				ID: "task-dispatch-poll", State: a2a.TaskCompleted,
				Artifacts: []a2a.Artifact{{Name: "result", Parts: []a2a.Part{{Kind: "text", Text: "async done"}}}},
			})
		default:
			t.Fatalf("unexpected method %q", req.Method)
		}
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.A2A.Agents = []config.A2AAgentConfig{{Name: "async-bot", URL: srv.URL}}

	reg := a2a.NewRegistry()
	reg.Add("async-bot", a2a.AgentCard{
		Name:   "async-bot",
		Skills: []a2a.Skill{{ID: "task", Tags: []string{"async"}}},
	})

	tool := NewDispatchTool(cfg, reg)
	tool.httpClient = srv.Client()
	tool.pollEvery = time.Millisecond
	tool.pollFor = time.Second

	input, err := json.Marshal(map[string]string{"task_description": "async task"})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "async done")
	assert.GreaterOrEqual(t, getTaskCalls.Load(), int32(2))
}
