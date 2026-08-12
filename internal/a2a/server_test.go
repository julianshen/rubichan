package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider/anthropic"
	"github.com/julianshen/rubichan/internal/testutil"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedReplySSE builds a minimal Anthropic streaming SSE body that always
// answers with one text_delta of reply, in the exact event sequence
// internal/provider/anthropic's own tests use (see
// internal/provider/anthropic/provider_test.go's TestStreamTextResponse).
func fixedReplySSE(reply string) string {
	return fmt.Sprintf(`event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-5","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%q}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":5}}

event: message_stop
data: {"type":"message_stop"}

`, reply)
}

// newTestAgent builds a minimal Agent against a stub Anthropic server that
// always answers with a fixed text reply, so server tests don't need a
// real LLM. Follows internal/provider/anthropic/provider_test.go's own
// pattern exactly: New(baseURL, apiKey) + SetHTTPClient, not a
// functional-options constructor.
func newTestAgent(t *testing.T, reply string) *agent.Agent {
	t.Helper()
	body := fixedReplySSE(reply)
	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))

	p := anthropic.New(server.URL, "test-api-key")
	p.SetHTTPClient(server.Client())

	cfg := config.DefaultConfig()
	approve := func(context.Context, string, json.RawMessage) (bool, error) { return true, nil }
	return agent.New(p, tools.NewRegistry(), approve, cfg)
}

type memoryTaskStore struct{ tasks map[string]Task }

func newMemoryTaskStore() *memoryTaskStore { return &memoryTaskStore{tasks: map[string]Task{}} }

func (m *memoryTaskStore) Create(_ context.Context, t Task) error { m.tasks[t.ID] = t; return nil }
func (m *memoryTaskStore) Get(_ context.Context, id string) (Task, error) {
	t, ok := m.tasks[id]
	if !ok {
		return Task{}, fmt.Errorf("task %s not found", id)
	}
	return t, nil
}
func (m *memoryTaskStore) Update(_ context.Context, t Task) error { m.tasks[t.ID] = t; return nil }

func TestServerMessageSendCompletes(t *testing.T) {
	a := newTestAgent(t, "42")
	store := newMemoryTaskStore()
	srv := NewServer(a, store, AgentCard{Name: "rubichan"}, false, "")

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody, _ := json.Marshal(RPCRequest{
		JSONRPC: "2.0", ID: 1, Method: "message/send",
		Params: mustMarshal(t, struct {
			Message Message `json:"message"`
		}{Message: Message{Role: "user", Parts: []Part{{Kind: "text", Text: "what is 6*7?"}}}}),
	})

	resp, err := http.Post(ts.URL+"/", "application/json", strings.NewReader(string(reqBody)))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"),
		"success responses must set Content-Type like writeError already does")

	var rpcResp RPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rpcResp))
	require.Nil(t, rpcResp.Error)

	var task Task
	require.NoError(t, json.Unmarshal(rpcResp.Result, &task))
	assert.Equal(t, TaskCompleted, task.State)
	assert.Contains(t, task.Artifacts[0].Parts[0].Text, "42")
}

func TestServerMessageStreamPushesEvents(t *testing.T) {
	a := newTestAgent(t, "42")
	store := newMemoryTaskStore()
	srv := NewServer(a, store, AgentCard{Name: "rubichan"}, false, "")

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody, _ := json.Marshal(RPCRequest{
		JSONRPC: "2.0", ID: 1, Method: "message/stream",
		Params: mustMarshal(t, sendParams{Message: Message{Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}}}}),
	})

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/", strings.NewReader(string(reqBody)))
	require.NoError(t, err)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	events := parseSSEEvents(resp.Body)
	var last Task
	for raw := range events {
		require.NoError(t, json.Unmarshal(raw, &last))
	}
	assert.Equal(t, TaskCompleted, last.State)
}

func TestServerHandleMessageStreamInvalidParams(t *testing.T) {
	a := newTestAgent(t, "unused")
	srv := NewServer(a, newMemoryTaskStore(), AgentCard{Name: "rubichan"}, false, "")

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody, _ := json.Marshal(RPCRequest{
		JSONRPC: "2.0", ID: 1, Method: "message/stream",
		Params: json.RawMessage(`{"message": 123}`),
	})
	resp, err := http.Post(ts.URL+"/", "application/json", strings.NewReader(string(reqBody)))
	require.NoError(t, err)
	defer resp.Body.Close()

	var rpcResp RPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rpcResp))
	require.NotNil(t, rpcResp.Error)
	assert.Equal(t, -32602, rpcResp.Error.Code)
}

// TestServerMessageStreamLogsTaskStoreWriteFailures is the message/stream
// analog of TestServerMessageSendLogsTaskStoreWriteFailures: TaskStore
// Create/Update failures during streaming must be logged as warnings
// without breaking the SSE response.
func TestServerMessageStreamLogsTaskStoreWriteFailures(t *testing.T) {
	a := newTestAgent(t, "ok")
	srv := NewServer(a, &erroringTaskStore{
		createErr: errors.New("create boom"),
		updateErr: errors.New("update boom"),
	}, AgentCard{Name: "rubichan"}, false, "")

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	var logBuf bytes.Buffer
	prevOutput := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&logBuf)
	defer func() {
		log.SetOutput(prevOutput)
		log.SetFlags(prevFlags)
	}()

	reqBody, _ := json.Marshal(RPCRequest{
		JSONRPC: "2.0", ID: 1, Method: "message/stream",
		Params: mustMarshal(t, sendParams{Message: Message{Role: "user", Parts: []Part{{Kind: "text", Text: "test"}}}}),
	})

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/", strings.NewReader(string(reqBody)))
	require.NoError(t, err)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	events := parseSSEEvents(resp.Body)
	var last Task
	for raw := range events {
		require.NoError(t, json.Unmarshal(raw, &last))
	}
	assert.Equal(t, TaskCompleted, last.State, "task should complete despite store failures")

	logged := logBuf.String()
	assert.Contains(t, logged, "warning:", "failed store operations must be logged as warnings")
	assert.Contains(t, logged, "create boom", "create failure must be logged")
	assert.Contains(t, logged, "update boom", "update failures must be logged")
}

func TestServerHandleAgentCard(t *testing.T) {
	a := newTestAgent(t, "unused")
	srv := NewServer(a, newMemoryTaskStore(), AgentCard{Name: "rubichan"}, false, "")

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/agent.json")
	require.NoError(t, err)
	defer resp.Body.Close()

	var card AgentCard
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&card))
	assert.Equal(t, "rubichan", card.Name)
}

// TestServerHandleRPCRejectsOversizedBody is a regression test for the
// server decoding JSON-RPC request bodies with no size cap: an
// unauthenticated or misbehaving client could stream an unbounded body and
// exhaust server memory. The request is built directly (httptest.NewRequest
// + ResponseRecorder, no real socket) rather than over httptest.NewServer,
// because a real TCP round trip with a body this size is prone to blocking
// or broken-pipe flakiness once the server stops reading mid-decode.
//
// The oversized payload is a single large JSON string value (not just
// padding after a small valid object) so the decoder must actually consume
// bytes past the cap to finish parsing — otherwise json.Decoder could stop
// early after parsing a small leading value and never notice the excess.
func TestServerHandleRPCRejectsOversizedBody(t *testing.T) {
	srv := NewServer(nil, newMemoryTaskStore(), AgentCard{Name: "rubichan"}, false, "")

	huge := strings.Repeat("a", maxRPCRequestBytes+1024)
	body := `{"jsonrpc":"2.0","id":1,"method":"message/send","params":"` + huge + `"}`

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	rec := httptest.NewRecorder()

	srv.handleRPC(rec, req)

	var rpcResp RPCResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&rpcResp))
	require.NotNil(t, rpcResp.Error, "oversized body must be rejected, not decoded")
	assert.Equal(t, -32700, rpcResp.Error.Code, "oversized body should surface as the existing parse-error path")
}

func TestServerHandleRPCParseError(t *testing.T) {
	a := newTestAgent(t, "unused")
	srv := NewServer(a, newMemoryTaskStore(), AgentCard{Name: "rubichan"}, false, "")

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/", "application/json", strings.NewReader("not json"))
	require.NoError(t, err)
	defer resp.Body.Close()

	var rpcResp RPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rpcResp))
	require.NotNil(t, rpcResp.Error)
	assert.Equal(t, -32700, rpcResp.Error.Code)
}

func TestServerHandleRPCMethodNotFound(t *testing.T) {
	a := newTestAgent(t, "unused")
	srv := NewServer(a, newMemoryTaskStore(), AgentCard{Name: "rubichan"}, false, "")

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody, _ := json.Marshal(RPCRequest{JSONRPC: "2.0", ID: 7, Method: "nope"})
	resp, err := http.Post(ts.URL+"/", "application/json", strings.NewReader(string(reqBody)))
	require.NoError(t, err)
	defer resp.Body.Close()

	var rpcResp RPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rpcResp))
	require.NotNil(t, rpcResp.Error)
	assert.Equal(t, -32601, rpcResp.Error.Code)
	assert.Equal(t, int64(7), rpcResp.ID)
}

func TestServerHandleMessageSendInvalidParams(t *testing.T) {
	a := newTestAgent(t, "unused")
	srv := NewServer(a, newMemoryTaskStore(), AgentCard{Name: "rubichan"}, false, "")

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody, _ := json.Marshal(RPCRequest{
		JSONRPC: "2.0", ID: 1, Method: "message/send",
		Params: json.RawMessage(`{"message": 123}`),
	})
	resp, err := http.Post(ts.URL+"/", "application/json", strings.NewReader(string(reqBody)))
	require.NoError(t, err)
	defer resp.Body.Close()

	var rpcResp RPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rpcResp))
	require.NotNil(t, rpcResp.Error)
	assert.Equal(t, -32602, rpcResp.Error.Code)
}

// newFailingTestAgent builds an Agent against a stub Anthropic server that
// always answers with a non-retryable HTTP error (400 invalid_request_error,
// per internal/provider/apierror.go's classification), so Agent.Turn emits
// an "error" event mid-stream on the first attempt with no retry backoff —
// exercising runTurn's failure path without slowing the test down.
func newFailingTestAgent(t *testing.T) *agent.Agent {
	t.Helper()
	server := testutil.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"bad request"}}`))
	}))

	p := anthropic.New(server.URL, "test-api-key")
	p.SetHTTPClient(server.Client())

	cfg := config.DefaultConfig()
	approve := func(context.Context, string, json.RawMessage) (bool, error) { return true, nil }
	return agent.New(p, tools.NewRegistry(), approve, cfg)
}

func TestServerMessageSendTurnFails(t *testing.T) {
	a := newFailingTestAgent(t)
	srv := NewServer(a, newMemoryTaskStore(), AgentCard{Name: "rubichan"}, false, "")

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody, _ := json.Marshal(RPCRequest{
		JSONRPC: "2.0", ID: 1, Method: "message/send",
		Params: mustMarshal(t, struct {
			Message Message `json:"message"`
		}{Message: Message{Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}}}}),
	})

	resp, err := http.Post(ts.URL+"/", "application/json", strings.NewReader(string(reqBody)))
	require.NoError(t, err)
	defer resp.Body.Close()

	var rpcResp RPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rpcResp))
	require.Nil(t, rpcResp.Error)

	var task Task
	require.NoError(t, json.Unmarshal(rpcResp.Result, &task))
	assert.Equal(t, TaskFailed, task.State)
	assert.NotEmpty(t, task.Error)
}

func TestServerRunTurnRecoversPanic(t *testing.T) {
	srv := NewServer(nil, newMemoryTaskStore(), AgentCard{Name: "rubichan"}, false, "")

	task := Task{ID: "task-panic", State: TaskWorking}
	result := srv.runTurn(context.Background(), task, "hi")

	assert.Equal(t, TaskFailed, result.State)
	assert.Contains(t, result.Error, "panic")
}

func TestServerRequiresAuthWhenConfigured(t *testing.T) {
	a := newTestAgent(t, "42")
	store := newMemoryTaskStore()
	srv := NewServer(a, store, AgentCard{Name: "rubichan"}, true, "secret")

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody, _ := json.Marshal(RPCRequest{JSONRPC: "2.0", ID: 1, Method: "tasks/get"})
	resp, err := http.Post(ts.URL+"/", "application/json", strings.NewReader(string(reqBody)))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestServerAcceptsMatchingBearerToken(t *testing.T) {
	a := newTestAgent(t, "42")
	store := newMemoryTaskStore()
	srv := NewServer(a, store, AgentCard{Name: "rubichan"}, true, "secret")

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	require.NoError(t, store.Create(context.Background(), Task{ID: "task-1", State: TaskWorking}))

	reqBody, _ := json.Marshal(RPCRequest{
		JSONRPC: "2.0", ID: 1, Method: "tasks/get",
		Params: mustMarshal(t, struct {
			ID string `json:"id"`
		}{ID: "task-1"}),
	})
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/", strings.NewReader(string(reqBody)))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer secret")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var rpcResp RPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rpcResp))
	var task Task
	require.NoError(t, json.Unmarshal(rpcResp.Result, &task))
	assert.Equal(t, TaskWorking, task.State)
}

// TestServerRejectsEmptyConfiguredAPIKey is a regression test: authorized()
// must never treat an empty configured apiKey as "anything goes." A Server
// constructed with requireAuth=true and apiKey="" is a caller mistake (the
// one current production call site avoids it via config.ResolveAPIKey), but
// the type itself must not silently bypass auth for it.
//
// This exercises authorized() directly rather than over a real HTTP round
// trip: net/textproto's header parsing trims trailing OWS from header
// values, so a literal "Bearer " sent on the wire arrives as "Bearer" by
// the time the server sees it — masking the bug this test targets.
// Constructing the *http.Request in-process with Header.Set preserves the
// exact trailing space.
func TestServerRejectsEmptyConfiguredAPIKey(t *testing.T) {
	srv := NewServer(nil, newMemoryTaskStore(), AgentCard{Name: "rubichan"}, true, "")

	// A client sending exactly "Bearer " (empty token) must not authorize
	// against an empty configured apiKey.
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer ")
	assert.False(t, srv.authorized(req), "empty token against empty configured key must be rejected")

	// A non-empty wrong token against an empty configured key must also be
	// rejected (this should already work, but confirm it stays true).
	req2 := httptest.NewRequest(http.MethodPost, "/", nil)
	req2.Header.Set("Authorization", "Bearer wrong-token")
	assert.False(t, srv.authorized(req2))
}

func TestServerTasksCancel(t *testing.T) {
	a := newTestAgent(t, "42")
	store := newMemoryTaskStore()
	srv := NewServer(a, store, AgentCard{Name: "rubichan"}, false, "")

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	require.NoError(t, store.Create(context.Background(), Task{ID: "task-1", State: TaskWorking}))

	reqBody, _ := json.Marshal(RPCRequest{
		JSONRPC: "2.0", ID: 1, Method: "tasks/cancel",
		Params: mustMarshal(t, struct {
			ID string `json:"id"`
		}{ID: "task-1"}),
	})
	resp, err := http.Post(ts.URL+"/", "application/json", strings.NewReader(string(reqBody)))
	require.NoError(t, err)
	defer resp.Body.Close()

	task, err := store.Get(context.Background(), "task-1")
	require.NoError(t, err)
	assert.Equal(t, TaskCanceled, task.State)
}

func TestServerHandleTasksGetInvalidParams(t *testing.T) {
	a := newTestAgent(t, "unused")
	srv := NewServer(a, newMemoryTaskStore(), AgentCard{Name: "rubichan"}, false, "")

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody, _ := json.Marshal(RPCRequest{
		JSONRPC: "2.0", ID: 1, Method: "tasks/get",
		Params: json.RawMessage(`{"id": 123}`),
	})
	resp, err := http.Post(ts.URL+"/", "application/json", strings.NewReader(string(reqBody)))
	require.NoError(t, err)
	defer resp.Body.Close()

	var rpcResp RPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rpcResp))
	require.NotNil(t, rpcResp.Error)
	assert.Equal(t, -32602, rpcResp.Error.Code)
}

func TestServerHandleTasksGetNotFound(t *testing.T) {
	a := newTestAgent(t, "unused")
	srv := NewServer(a, newMemoryTaskStore(), AgentCard{Name: "rubichan"}, false, "")

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody, _ := json.Marshal(RPCRequest{
		JSONRPC: "2.0", ID: 1, Method: "tasks/get",
		Params: mustMarshal(t, taskIDParams{ID: "missing"}),
	})
	resp, err := http.Post(ts.URL+"/", "application/json", strings.NewReader(string(reqBody)))
	require.NoError(t, err)
	defer resp.Body.Close()

	var rpcResp RPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rpcResp))
	require.NotNil(t, rpcResp.Error)
	assert.Equal(t, -32000, rpcResp.Error.Code)
}

func TestServerHandleTasksCancelInvalidParams(t *testing.T) {
	a := newTestAgent(t, "unused")
	srv := NewServer(a, newMemoryTaskStore(), AgentCard{Name: "rubichan"}, false, "")

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody, _ := json.Marshal(RPCRequest{
		JSONRPC: "2.0", ID: 1, Method: "tasks/cancel",
		Params: json.RawMessage(`{"id": 123}`),
	})
	resp, err := http.Post(ts.URL+"/", "application/json", strings.NewReader(string(reqBody)))
	require.NoError(t, err)
	defer resp.Body.Close()

	var rpcResp RPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rpcResp))
	require.NotNil(t, rpcResp.Error)
	assert.Equal(t, -32602, rpcResp.Error.Code)
}

func TestServerHandleTasksCancelNotFound(t *testing.T) {
	a := newTestAgent(t, "unused")
	srv := NewServer(a, newMemoryTaskStore(), AgentCard{Name: "rubichan"}, false, "")

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody, _ := json.Marshal(RPCRequest{
		JSONRPC: "2.0", ID: 1, Method: "tasks/cancel",
		Params: mustMarshal(t, taskIDParams{ID: "missing"}),
	})
	resp, err := http.Post(ts.URL+"/", "application/json", strings.NewReader(string(reqBody)))
	require.NoError(t, err)
	defer resp.Body.Close()

	var rpcResp RPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rpcResp))
	require.NotNil(t, rpcResp.Error)
	assert.Equal(t, -32000, rpcResp.Error.Code)
}

// cancelUpdateErrorStore wraps a memoryTaskStore but forces Update to fail,
// so a seeded task can still be Get() during tasks/cancel while exercising
// the TaskStore-error-logging path this task's context calls for on any new
// Update call (the existing log.Printf("warning: ...") convention, rather
// than discarding the error as the brief's own snippet does).
type cancelUpdateErrorStore struct {
	*memoryTaskStore
	updateErr error
}

func (c *cancelUpdateErrorStore) Update(_ context.Context, _ Task) error { return c.updateErr }

func TestServerHandleTasksCancelLogsTaskStoreUpdateFailure(t *testing.T) {
	a := newTestAgent(t, "unused")
	inner := newMemoryTaskStore()
	require.NoError(t, inner.Create(context.Background(), Task{ID: "task-1", State: TaskWorking}))
	store := &cancelUpdateErrorStore{memoryTaskStore: inner, updateErr: errors.New("update boom")}
	srv := NewServer(a, store, AgentCard{Name: "rubichan"}, false, "")

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	var logBuf bytes.Buffer
	prevOutput := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&logBuf)
	defer func() {
		log.SetOutput(prevOutput)
		log.SetFlags(prevFlags)
	}()

	reqBody, _ := json.Marshal(RPCRequest{
		JSONRPC: "2.0", ID: 1, Method: "tasks/cancel",
		Params: mustMarshal(t, taskIDParams{ID: "task-1"}),
	})
	resp, err := http.Post(ts.URL+"/", "application/json", strings.NewReader(string(reqBody)))
	require.NoError(t, err)
	defer resp.Body.Close()

	var rpcResp RPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rpcResp))
	require.Nil(t, rpcResp.Error, "tasks/cancel should still return the task despite store failures")

	var task Task
	require.NoError(t, json.Unmarshal(rpcResp.Result, &task))
	assert.Equal(t, TaskCanceled, task.State, "response should reflect canceled state even if persisting it failed")

	logged := logBuf.String()
	assert.Contains(t, logged, "warning:", "failed store update must be logged as a warning")
	assert.Contains(t, logged, "update boom", "update failure must be logged")
}

// erroringTaskStore is a TaskStore fake whose Create/Update always fail,
// for exercising the server's error-visibility logging.
type erroringTaskStore struct {
	createErr error
	updateErr error
}

func (e *erroringTaskStore) Create(_ context.Context, _ Task) error { return e.createErr }
func (e *erroringTaskStore) Get(_ context.Context, id string) (Task, error) {
	return Task{}, fmt.Errorf("task %s not found", id)
}
func (e *erroringTaskStore) Update(_ context.Context, _ Task) error { return e.updateErr }

// TestServerMessageSendLogsTaskStoreWriteFailures is a regression test for
// TaskStore write errors being silently swallowed. Create/Update failures
// must be logged as warnings, without failing the message/send response.
func TestServerMessageSendLogsTaskStoreWriteFailures(t *testing.T) {
	a := newTestAgent(t, "ok")
	srv := NewServer(a, &erroringTaskStore{
		createErr: errors.New("create boom"),
		updateErr: errors.New("update boom"),
	}, AgentCard{Name: "rubichan"}, false, "")

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	var logBuf bytes.Buffer
	prevOutput := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&logBuf)
	defer func() {
		log.SetOutput(prevOutput)
		log.SetFlags(prevFlags)
	}()

	reqBody, _ := json.Marshal(RPCRequest{
		JSONRPC: "2.0", ID: 1, Method: "message/send",
		Params: mustMarshal(t, struct {
			Message Message `json:"message"`
		}{Message: Message{Role: "user", Parts: []Part{{Kind: "text", Text: "test"}}}}),
	})

	resp, err := http.Post(ts.URL+"/", "application/json", strings.NewReader(string(reqBody)))
	require.NoError(t, err)
	defer resp.Body.Close()

	var rpcResp RPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rpcResp))
	require.Nil(t, rpcResp.Error, "message/send should not fail when TaskStore fails")

	var task Task
	require.NoError(t, json.Unmarshal(rpcResp.Result, &task))
	assert.Equal(t, TaskCompleted, task.State, "task should complete despite store failures")
	assert.Contains(t, task.Artifacts[0].Parts[0].Text, "ok")

	logged := logBuf.String()
	assert.Contains(t, logged, "warning:", "failed store operations must be logged as warnings")
	assert.Contains(t, logged, "create boom", "create failure must be logged")
	assert.Contains(t, logged, "update boom", "update failures must be logged")
}
