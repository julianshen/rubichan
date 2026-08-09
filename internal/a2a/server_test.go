package a2a

import (
	"context"
	"encoding/json"
	"fmt"
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

	var rpcResp RPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rpcResp))
	require.Nil(t, rpcResp.Error)

	var task Task
	require.NoError(t, json.Unmarshal(rpcResp.Result, &task))
	assert.Equal(t, TaskCompleted, task.State)
	assert.Contains(t, task.Artifacts[0].Parts[0].Text, "42")
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
