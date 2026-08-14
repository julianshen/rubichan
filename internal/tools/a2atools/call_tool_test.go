package a2atools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/a2a"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/testutil"
	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCallToolSyncCompletion(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var req a2a.RPCRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		result, _ := json.Marshal(a2a.Task{
			ID: "task-1", State: a2a.TaskCompleted,
			Artifacts: []a2a.Artifact{{Name: "result", Parts: []a2a.Part{{Kind: "text", Text: "42"}}}},
		})
		resp := a2a.RPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	cfg := config.DefaultConfig()
	tool := NewCallTool(cfg, s)
	tool.httpClient = srv.Client()

	input, err := json.Marshal(map[string]string{"agent": srv.URL, "message": "what is 6*7?"})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "42")
}

// TestCallToolRejectsPrivateAddress guards against the SSRF finding shared
// by all three a2atools tools: a raw "agent" URL argument must not reach
// internal/private addresses. httpClient is left nil to exercise the
// production default (defaultA2AHTTPClient), not the httpClient-override
// test seam used elsewhere in this file.
func TestCallToolRejectsPrivateAddress(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	cfg := config.DefaultConfig()
	tool := NewCallTool(cfg, s)

	input, err := json.Marshal(map[string]string{"agent": "http://127.0.0.1:1/", "message": "hi"})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "private or local addresses")
}

// writeRPCResult encodes v as the result of a successful JSON-RPC response
// matching req's ID.
func writeRPCResult(t *testing.T, w http.ResponseWriter, req a2a.RPCRequest, v any) {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	resp := a2a.RPCResponse{JSONRPC: "2.0", ID: req.ID, Result: raw}
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(resp))
}

// writeRPCError encodes a JSON-RPC error response matching req's ID.
func writeRPCError(t *testing.T, w http.ResponseWriter, req a2a.RPCRequest, msg string) {
	t.Helper()
	resp := a2a.RPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &a2a.RPCError{Code: -32000, Message: msg}}
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(resp))
}

// decodeRPCRequest reads and decodes the JSON-RPC request body.
func decodeRPCRequest(t *testing.T, r *http.Request) a2a.RPCRequest {
	t.Helper()
	var req a2a.RPCRequest
	require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
	return req
}

// TestCallToolPollLoop exercises the poll loop itself: the task is not
// terminal on Send (state "working"), requires two GetTask polls before
// reaching TaskCompleted.
func TestCallToolPollLoop(t *testing.T) {
	var getTaskCalls atomic.Int32

	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPCRequest(t, r)
		switch req.Method {
		case "message/send":
			writeRPCResult(t, w, req, a2a.Task{ID: "task-poll", State: a2a.TaskWorking})
		case "tasks/get":
			n := getTaskCalls.Add(1)
			if n < 2 {
				writeRPCResult(t, w, req, a2a.Task{ID: "task-poll", State: a2a.TaskWorking})
				return
			}
			writeRPCResult(t, w, req, a2a.Task{
				ID: "task-poll", State: a2a.TaskCompleted,
				Artifacts: []a2a.Artifact{{Name: "result", Parts: []a2a.Part{{Kind: "text", Text: "done"}}}},
			})
		default:
			t.Fatalf("unexpected method %q", req.Method)
		}
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	tool := NewCallTool(config.DefaultConfig(), s)
	tool.httpClient = srv.Client()
	tool.pollEvery = time.Millisecond
	tool.pollFor = time.Second

	input, err := json.Marshal(map[string]string{"agent": srv.URL, "message": "poll me"})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "done")
	assert.GreaterOrEqual(t, getTaskCalls.Load(), int32(2))
}

// TestCallToolTerminalNonCompletedState exercises the "task ended in
// state" error-result branch: the task reaches a terminal state other
// than TaskCompleted.
func TestCallToolTerminalNonCompletedState(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPCRequest(t, r)
		require.Equal(t, "message/send", req.Method)
		writeRPCResult(t, w, req, a2a.Task{ID: "task-failed", State: a2a.TaskFailed, Error: "boom"})
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	tool := NewCallTool(config.DefaultConfig(), s)
	tool.httpClient = srv.Client()

	input, err := json.Marshal(map[string]string{"agent": srv.URL, "message": "fail me"})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "task ended in state")
	assert.Contains(t, result.Content, "failed")
	assert.Contains(t, result.Content, "boom")
}

// TestCallToolPollErrorMidPoll exercises the "poll failed" error branch:
// GetTask returns a JSON-RPC error after the task starts non-terminal.
func TestCallToolPollErrorMidPoll(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPCRequest(t, r)
		switch req.Method {
		case "message/send":
			writeRPCResult(t, w, req, a2a.Task{ID: "task-err", State: a2a.TaskWorking})
		case "tasks/get":
			writeRPCError(t, w, req, "remote exploded")
		default:
			t.Fatalf("unexpected method %q", req.Method)
		}
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	tool := NewCallTool(config.DefaultConfig(), s)
	tool.httpClient = srv.Client()
	tool.pollEvery = time.Millisecond
	tool.pollFor = time.Second

	input, err := json.Marshal(map[string]string{"agent": srv.URL, "message": "poll err"})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "poll failed")
	assert.Contains(t, result.Content, "remote exploded")
}

// TestCallToolContextCancellationDuringPoll exercises the ctx.Done()
// branch of the poll loop's select: the caller's context is canceled
// while waiting between polls.
func TestCallToolContextCancellationDuringPoll(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPCRequest(t, r)
		require.Equal(t, "message/send", req.Method)
		writeRPCResult(t, w, req, a2a.Task{ID: "task-cancel", State: a2a.TaskWorking})
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	tool := NewCallTool(config.DefaultConfig(), s)
	tool.httpClient = srv.Client()
	tool.pollEvery = time.Hour // never fires; cancellation must win the select
	tool.pollFor = time.Hour

	ctx, cancel := context.WithCancel(context.Background())

	input, err := json.Marshal(map[string]string{"agent": srv.URL, "message": "cancel me"})
	require.NoError(t, err)

	type execOutcome struct {
		result agentsdk.ToolResult
		err    error
	}
	resultCh := make(chan execOutcome, 1)
	go func() {
		result, execErr := tool.Execute(ctx, input)
		resultCh <- execOutcome{result, execErr}
	}()

	time.Sleep(20 * time.Millisecond) // let Execute reach the poll wait
	cancel()

	select {
	case o := <-resultCh:
		require.NoError(t, o.err)
		assert.True(t, o.result.IsError)
		assert.Contains(t, o.result.Content, "canceled")
	case <-time.After(5 * time.Second):
		t.Fatal("Execute did not return after context cancellation")
	}
}

// TestCallToolPollDeadlineBoundsStalledCall is a regression test for the
// bug where a stalled GetTask call (remote accepts the connection but
// never responds) could block Execute indefinitely, ignoring pollFor.
// Each poll call must now be bounded by its own per-call context.
func TestCallToolPollDeadlineBoundsStalledCall(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPCRequest(t, r)
		switch req.Method {
		case "message/send":
			writeRPCResult(t, w, req, a2a.Task{ID: "task-stall", State: a2a.TaskWorking})
		case "tasks/get":
			<-r.Context().Done() // simulate a remote that never responds
		default:
			t.Fatalf("unexpected method %q", req.Method)
		}
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	tool := NewCallTool(config.DefaultConfig(), s)
	tool.httpClient = srv.Client()
	tool.pollEvery = 5 * time.Millisecond
	tool.pollFor = 100 * time.Millisecond

	input, err := json.Marshal(map[string]string{"agent": srv.URL, "message": "stall me"})
	require.NoError(t, err)

	type execOutcome struct {
		result agentsdk.ToolResult
		err    error
	}
	resultCh := make(chan execOutcome, 1)
	start := time.Now()
	go func() {
		result, execErr := tool.Execute(context.Background(), input)
		resultCh <- execOutcome{result, execErr}
	}()

	select {
	case o := <-resultCh:
		require.NoError(t, o.err)
		elapsed := time.Since(start)
		// Worst case is roughly pollFor plus one per-call timeout
		// period; give generous headroom above pollFor (100ms) but
		// stay far below "forever".
		assert.Less(t, elapsed, 2*time.Second, "Execute should not block far past pollFor")
		assert.True(t, o.result.IsError)
	case <-time.After(5 * time.Second):
		t.Fatal("Execute blocked indefinitely on a stalled GetTask call")
	}
}

// TestCallToolLogsTaskStoreWriteFailures is a regression test for
// TaskStore write errors being silently swallowed. Create/Update failures
// must be logged as warnings, without failing the tool call.
func TestCallToolLogsTaskStoreWriteFailures(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPCRequest(t, r)
		switch req.Method {
		case "message/send":
			writeRPCResult(t, w, req, a2a.Task{ID: "task-store-err", State: a2a.TaskWorking})
		case "tasks/get":
			writeRPCResult(t, w, req, a2a.Task{
				ID: "task-store-err", State: a2a.TaskCompleted,
				Artifacts: []a2a.Artifact{{Name: "result", Parts: []a2a.Part{{Kind: "text", Text: "ok"}}}},
			})
		default:
			t.Fatalf("unexpected method %q", req.Method)
		}
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	tool := NewCallTool(config.DefaultConfig(), s)
	tool.httpClient = srv.Client()
	tool.pollEvery = time.Millisecond
	tool.pollFor = time.Second
	tool.tasks = &erroringTaskStore{
		createErr: errors.New("create boom"),
		updateErr: errors.New("update boom"),
	}

	var logBuf bytes.Buffer
	prevOutput := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&logBuf)
	defer func() {
		log.SetOutput(prevOutput)
		log.SetFlags(prevFlags)
	}()

	input, err := json.Marshal(map[string]string{"agent": srv.URL, "message": "store err"})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError, "TaskStore write failures must not fail the tool call")
	assert.Contains(t, result.Content, "ok")

	logged := logBuf.String()
	assert.Contains(t, logged, "warning:")
	assert.Contains(t, logged, "create boom")
	assert.Contains(t, logged, "update boom")
}

// erroringTaskStore is a a2a.TaskStore fake whose Create/Update always
// fail, for exercising the tool's error-visibility logging.
type erroringTaskStore struct {
	createErr error
	updateErr error
}

func (e *erroringTaskStore) Create(_ context.Context, _ a2a.Task) error { return e.createErr }
func (e *erroringTaskStore) Get(_ context.Context, _ string) (a2a.Task, error) {
	return a2a.Task{}, nil
}
func (e *erroringTaskStore) Update(_ context.Context, _ a2a.Task) error { return e.updateErr }

func TestCallToolName(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	tool := NewCallTool(config.DefaultConfig(), s)
	assert.Equal(t, "a2a_call", tool.Name())
}

func TestCallToolDescription(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	tool := NewCallTool(config.DefaultConfig(), s)
	desc := tool.Description()
	assert.NotEmpty(t, desc)
	assert.Contains(t, desc, "Agent2Agent")
}

func TestCallToolInputSchema(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	tool := NewCallTool(config.DefaultConfig(), s)
	schema := tool.InputSchema()

	var schemaObj map[string]interface{}
	require.NoError(t, json.Unmarshal(schema, &schemaObj))
	assert.Equal(t, "object", schemaObj["type"])
}

func TestCallToolInvalidInput(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	tool := NewCallTool(config.DefaultConfig(), s)
	result, err := tool.Execute(context.Background(), []byte("not json"))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid input")
}

func TestCallToolUnknownAgent(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	tool := NewCallTool(config.DefaultConfig(), s)
	input, err := json.Marshal(map[string]string{"agent": "not-configured", "message": "hi"})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

// TestCallToolSendFails exercises the "call failed" branch: the initial
// Send call itself errors out (here, because the target server has
// already been closed).
func TestCallToolSendFails(t *testing.T) {
	handler := http.NewServeMux()
	srv := testutil.NewServer(t, handler)
	srv.Close() // closed before use, so any call to it fails

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	tool := NewCallTool(config.DefaultConfig(), s)
	tool.httpClient = srv.Client()

	input, err := json.Marshal(map[string]string{"agent": srv.URL, "message": "hello"})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "call failed")
}

// TestCallToolPollDeadlineExceededWhileWorking exercises the "gave up
// waiting" branch: every poll succeeds (no single call stalls), but the
// task never reaches a terminal state before pollFor elapses.
func TestCallToolPollDeadlineExceededWhileWorking(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPCRequest(t, r)
		switch req.Method {
		case "message/send", "tasks/get":
			writeRPCResult(t, w, req, a2a.Task{ID: "task-never-done", State: a2a.TaskWorking})
		default:
			t.Fatalf("unexpected method %q", req.Method)
		}
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	tool := NewCallTool(config.DefaultConfig(), s)
	tool.httpClient = srv.Client()
	// pollEvery deliberately overshoots pollFor by a wide margin so the
	// deadline is unambiguously (not just barely) elapsed by the time the
	// select wakes up, regardless of scheduling jitter under a loaded
	// test run. That keeps this test exercising the clean "deadline
	// already passed, don't bother calling" path deterministically,
	// rather than racing a real per-call timeout (see
	// TestCallToolPollDeadlineBoundsStalledCall for that scenario).
	tool.pollEvery = 200 * time.Millisecond
	tool.pollFor = 10 * time.Millisecond

	input, err := json.Marshal(map[string]string{"agent": srv.URL, "message": "never finish"})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "gave up waiting")
	assert.Contains(t, result.Content, "working")
}
