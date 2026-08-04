package acp_test

import (
	"context"
	"encoding/json"
	"io"
	"strconv"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/acp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// peer is the far side of a Conn: it reads what the Conn writes and writes what
// the Conn reads. Tests drive it directly, standing in for an editor.
type peer struct {
	toConn   *io.PipeWriter
	fromConn *json.Decoder
}

// newConnPair wires a Conn to a peer over two pipes and serves the Conn until
// the test ends.
func newConnPair(t *testing.T, registry *acp.CapabilityRegistry) (*acp.Conn, *peer) {
	t.Helper()

	connR, peerW := io.Pipe()
	peerR, connW := io.Pipe()

	c := acp.NewConn(connR, connW, registry)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Serve(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		_ = peerW.Close()
		_ = connW.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("Serve did not return after cancel")
		}
	})

	return c, &peer{toConn: peerW, fromConn: json.NewDecoder(peerR)}
}

// TestConnRequestAwaitsPeerResponse is the capability the old transport could
// not express: the agent originating a request and blocking on the client's
// answer. session/request_permission is the reason it matters — a tool approval
// is a round trip that happens *during* a turn, so a buffered request/response
// server cannot carry it.
func TestConnRequestAwaitsPeerResponse(t *testing.T) {
	t.Parallel()

	c, p := newConnPair(t, acp.NewCapabilityRegistry())

	type reply struct {
		result json.RawMessage
		err    error
	}
	replies := make(chan reply, 1)
	go func() {
		res, err := c.Request(context.Background(), "session/request_permission",
			map[string]any{"sessionId": "sess-1"})
		replies <- reply{res, err}
	}()

	// The peer sees a well-formed JSON-RPC request carrying an ID.
	var got acp.Request
	p.decode(t, &got)
	assert.Equal(t, "2.0", got.JSONRPC)
	assert.Equal(t, "session/request_permission", got.Method)
	require.NotNil(t, got.ID, "an agent-initiated request must be correlatable")

	// The peer answers it.
	_, err := p.toConn.Write(append(mustJSON(t, acp.Response{
		JSONRPC: "2.0",
		ID:      got.ID,
		Result:  rawPtr(t, `{"outcome":{"outcome":"selected","optionId":"allow_once"}}`),
	}), '\n'))
	require.NoError(t, err)

	select {
	case r := <-replies:
		require.NoError(t, r.err)
		assert.JSONEq(t, `{"outcome":{"outcome":"selected","optionId":"allow_once"}}`, string(r.result))
	case <-time.After(2 * time.Second):
		t.Fatal("Request never returned; the response was not correlated back to the caller")
	}
}

// decode reads one message from the Conn, failing the test rather than hanging
// if none arrives. A dropped reply is a real failure mode here, so it must
// surface as a failure and not a CI timeout.
func (p *peer) decode(t *testing.T, v any) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- p.fromConn.Decode(v) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("no message from Conn within 2s")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func rawPtr(t *testing.T, s string) *json.RawMessage {
	t.Helper()
	require.True(t, json.Valid([]byte(s)), "test fixture must be valid JSON")
	raw := json.RawMessage(s)
	return &raw
}

// TestConnServesInboundRequests is the other half of bidirectional dispatch:
// the same loop that correlates our responses must also answer the peer's
// calls. Without it a Conn could talk but not listen, which is the mirror of
// the defect it replaces.
func TestConnServesInboundRequests(t *testing.T) {
	t.Parallel()

	registry := acp.NewCapabilityRegistry()
	registry.RegisterMethod("session/prompt", func(params json.RawMessage) (json.RawMessage, error) {
		var got struct {
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(params, &got); err != nil {
			return nil, err
		}
		return json.RawMessage(`{"stopReason":"end_turn","echo":"` + got.SessionID + `"}`), nil
	})

	_, p := newConnPair(t, registry)

	_, err := p.toConn.Write(append(mustJSON(t, acp.Request{
		JSONRPC: "2.0",
		ID:      int64(7),
		Method:  "session/prompt",
		Params:  json.RawMessage(`{"sessionId":"sess-9"}`),
	}), '\n'))
	require.NoError(t, err)

	var resp acp.Response
	p.decode(t, &resp)
	assert.Equal(t, float64(7), resp.ID, "the reply must carry the request's id")
	require.Nil(t, resp.Error)
	require.NotNil(t, resp.Result)
	assert.JSONEq(t, `{"stopReason":"end_turn","echo":"sess-9"}`, string(*resp.Result))
}

// TestConnReportsUnknownInboundMethod pins that an unserved method produces a
// JSON-RPC error rather than silence. A peer that gets no answer blocks; the
// mode adapters deleted in #339 failed exactly this way, calling methods the
// server never registered.
func TestConnReportsUnknownInboundMethod(t *testing.T) {
	t.Parallel()

	_, p := newConnPair(t, acp.NewCapabilityRegistry())

	_, err := p.toConn.Write(append(mustJSON(t, acp.Request{
		JSONRPC: "2.0",
		ID:      int64(3),
		Method:  "agent/codeReview",
	}), '\n'))
	require.NoError(t, err)

	var resp acp.Response
	p.decode(t, &resp)
	require.NotNil(t, resp.Error, "an unserved method must be reported, not ignored")
	assert.Equal(t, acp.ErrorCodeMethodNotFound, resp.Error.Code)
}

// TestConnHandlerCanCallBackDuringRequest is the whole point of the slice.
// While serving session/prompt, the handler asks the peer for permission and
// waits for the answer — a round trip *inside* another round trip. The old
// StdioTransport could not do this at all: its read loop was occupied handling
// the outer request, so the inner reply could never be read, and the handler
// would deadlock against the loop that was waiting for it to return.
//
// This is why serveRequest dispatches into a goroutine. Without that, this test
// hangs.
func TestConnHandlerCanCallBackDuringRequest(t *testing.T) {
	t.Parallel()

	registry := acp.NewCapabilityRegistry()
	var c *acp.Conn
	registry.RegisterMethod("session/prompt", func(_ json.RawMessage) (json.RawMessage, error) {
		granted, err := c.Request(context.Background(), "session/request_permission",
			map[string]any{"sessionId": "sess-1"})
		if err != nil {
			return nil, err
		}
		var out struct {
			Outcome struct {
				OptionID string `json:"optionId"`
			} `json:"outcome"`
		}
		if err := json.Unmarshal(granted, &out); err != nil {
			return nil, err
		}
		return json.RawMessage(`{"stopReason":"end_turn","granted":"` + out.Outcome.OptionID + `"}`), nil
	})

	c, p := newConnPair(t, registry)

	// Client opens the turn.
	_, err := p.toConn.Write(append(mustJSON(t, acp.Request{
		JSONRPC: "2.0", ID: int64(1), Method: "session/prompt",
	}), '\n'))
	require.NoError(t, err)

	// Mid-turn, the agent asks for permission.
	var ask acp.Request
	p.decode(t, &ask)
	require.Equal(t, "session/request_permission", ask.Method,
		"the agent must be able to originate a request while still serving one")
	require.NotNil(t, ask.ID)

	// Client answers.
	_, err = p.toConn.Write(append(mustJSON(t, acp.Response{
		JSONRPC: "2.0", ID: ask.ID,
		Result: rawPtr(t, `{"outcome":{"outcome":"selected","optionId":"allow_once"}}`),
	}), '\n'))
	require.NoError(t, err)

	// Only now does the turn complete, carrying the permission decision.
	var done acp.Response
	p.decode(t, &done)
	assert.Equal(t, float64(1), done.ID, "the turn's own id, not the permission request's")
	require.Nil(t, done.Error)
	require.NotNil(t, done.Result)
	assert.JSONEq(t, `{"stopReason":"end_turn","granted":"allow_once"}`, string(*done.Result))
}

// TestConnNotificationGetsNoReply pins the comment in serveRequest: a request
// without an id is a notification, and answering one would put an unmatched
// response on the wire that the peer's dispatcher would log and drop.
func TestConnNotificationGetsNoReply(t *testing.T) {
	t.Parallel()

	called := make(chan struct{}, 1)
	registry := acp.NewCapabilityRegistry()
	registry.RegisterMethod("session/cancel", func(json.RawMessage) (json.RawMessage, error) {
		called <- struct{}{}
		return nil, nil
	})

	_, p := newConnPair(t, registry)

	_, err := p.toConn.Write([]byte(`{"jsonrpc":"2.0","method":"session/cancel"}` + "\n"))
	require.NoError(t, err)

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("notification was not delivered to its handler")
	}

	// Nothing should come back. Give the connection a moment to get it wrong.
	replied := make(chan struct{})
	go func() {
		var reply map[string]any
		if err := p.fromConn.Decode(&reply); err == nil {
			close(replied)
		}
	}()
	select {
	case <-replied:
		t.Fatal("a notification must not be answered")
	case <-time.After(200 * time.Millisecond):
	}
}

// TestConnRequestFailsWhenConnectionDrops pins failPending. Without it a caller
// blocked in Request waits forever on a stream that has already gone away —
// the stranding failure this codebase has hit before.
func TestConnRequestFailsWhenConnectionDrops(t *testing.T) {
	t.Parallel()

	connR, peerW := io.Pipe()
	// io.Discard, not a pipe: an io.Pipe write blocks until something reads it,
	// and this test never reads the Conn's output, so a pipe here would stall
	// Request inside write() and never exercise the drop path at all.
	c := acp.NewConn(connR, io.Discard, acp.NewCapabilityRegistry())

	served := make(chan struct{})
	go func() { defer close(served); _ = c.Serve(context.Background()) }()

	errs := make(chan error, 1)
	go func() {
		_, err := c.Request(context.Background(), "session/request_permission", nil)
		errs <- err
	}()

	// Let the request register before the stream ends.
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, peerW.Close())

	select {
	case err := <-errs:
		require.Error(t, err, "a dropped connection must fail the caller, not strand it")
		assert.ErrorIs(t, err, acp.ErrConnClosed)
	case <-time.After(2 * time.Second):
		t.Fatal("Request stranded after the connection dropped")
	}
	<-served
}

// TestConnRequestHonoursContextCancellation covers the caller giving up — a
// user pressing Ctrl-C at an approval prompt.
func TestConnRequestHonoursContextCancellation(t *testing.T) {
	t.Parallel()

	// Reads block forever (nothing is ever sent) and writes go nowhere, which is
	// exactly the state a caller is in while waiting on an approval prompt.
	blocked, stop := io.Pipe()
	c := acp.NewConn(blocked, io.Discard, acp.NewCapabilityRegistry())
	serveCtx, stopServe := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() { defer close(served); _ = c.Serve(serveCtx) }()
	t.Cleanup(func() {
		stopServe()
		_ = stop.Close()
		select {
		case <-served:
		case <-time.After(2 * time.Second):
			t.Error("Serve did not return after cancel")
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	errs := make(chan error, 1)
	go func() {
		_, err := c.Request(ctx, "session/request_permission", nil)
		errs <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errs:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("cancelling the context did not release Request")
	}
}

// TestConnSurvivesMalformedInput pins that garbage on the wire does not kill
// the loop. A connection that dies on one bad line takes the session with it.
func TestConnSurvivesMalformedInput(t *testing.T) {
	t.Parallel()

	registry := acp.NewCapabilityRegistry()
	registry.RegisterMethod("ping", func(json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"pong"`), nil
	})

	_, p := newConnPair(t, registry)

	_, err := p.toConn.Write([]byte("not json at all\n"))
	require.NoError(t, err)
	_, err = p.toConn.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n"))
	require.NoError(t, err)

	var resp acp.Response
	p.decode(t, &resp)
	require.NotNil(t, resp.Result)
	assert.JSONEq(t, `"pong"`, string(*resp.Result))
}

// TestConnRequestSurfacesPeerError pins that a JSON-RPC error response becomes
// a Go error rather than an empty success — the shape that let the deleted
// adapters look like they worked.
func TestConnRequestSurfacesPeerError(t *testing.T) {
	t.Parallel()

	c, p := newConnPair(t, acp.NewCapabilityRegistry())

	errs := make(chan error, 1)
	go func() {
		_, err := c.Request(context.Background(), "fs/read_text_file", nil)
		errs <- err
	}()

	var ask acp.Request
	p.decode(t, &ask)
	_, err := p.toConn.Write(append(mustJSON(t, acp.Response{
		JSONRPC: "2.0", ID: ask.ID,
		Error: &acp.RPCError{Code: acp.ErrorCodeInvalidParams, Message: "path is required"},
	}), '\n'))
	require.NoError(t, err)

	select {
	case err := <-errs:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "path is required")
	case <-time.After(2 * time.Second):
		t.Fatal("peer error never surfaced")
	}
}

// TestConnRequestAfterServeExitsFailsFast covers the window failPending alone
// does not close: a request registered *after* Serve has already returned has
// nothing left to fail it, so it waits on a channel no one will ever touch.
// With a background context that is a permanent hang.
func TestConnRequestAfterServeExitsFailsFast(t *testing.T) {
	t.Parallel()

	connR, peerW := io.Pipe()
	c := acp.NewConn(connR, io.Discard, acp.NewCapabilityRegistry())

	served := make(chan struct{})
	go func() { defer close(served); _ = c.Serve(context.Background()) }()

	require.NoError(t, peerW.Close())
	<-served // Serve has exited and already run failPending.

	errs := make(chan error, 1)
	go func() {
		_, err := c.Request(context.Background(), "session/request_permission", nil)
		errs <- err
	}()

	select {
	case err := <-errs:
		require.Error(t, err, "a request on a dead connection must fail, not wait forever")
	case <-time.After(2 * time.Second):
		t.Fatal("Request stranded: registered after Serve exited, so nothing will ever fail it")
	}
}

// TestConnDeliversNotificationsInOrder pins that notifications arrive at their
// handler in the order the peer sent them. session/update is a stream of turn
// events — agent_message_chunk, tool_call, tool_call_update — so reordering
// does not lose data, it renders the wrong turn. One goroutine per notification
// leaves the order to the scheduler.
func TestConnDeliversNotificationsInOrder(t *testing.T) {
	t.Parallel()

	const count = 50
	seen := make(chan string, count)
	registry := acp.NewCapabilityRegistry()
	registry.RegisterMethod("session/update", func(params json.RawMessage) (json.RawMessage, error) {
		var got struct {
			Seq string `json:"seq"`
		}
		if err := json.Unmarshal(params, &got); err != nil {
			return nil, err
		}
		seen <- got.Seq
		return nil, nil
	})

	_, p := newConnPair(t, registry)

	for i := 0; i < count; i++ {
		_, err := p.toConn.Write([]byte(
			`{"jsonrpc":"2.0","method":"session/update","params":{"seq":"` +
				strconv.Itoa(i) + `"}}` + "\n"))
		require.NoError(t, err)
	}

	for i := 0; i < count; i++ {
		select {
		case got := <-seen:
			require.Equal(t, strconv.Itoa(i), got,
				"notification %d arrived out of order; a reordered session/update stream renders the wrong turn", i)
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of %d notifications were delivered", i, count)
		}
	}
}

// TestConnShutdownIsNotHeldByABlockedNotification pins that one wedged handler
// cannot hold the whole connection open. The notification worker was drained
// before failPending ran — defers are LIFO and failPending was registered first
// — so a handler that never returns kept Serve from returning and left every
// pending Request waiting on a connection that was already finished.
func TestConnShutdownIsNotHeldByABlockedNotification(t *testing.T) {
	t.Parallel()

	wedged := make(chan struct{})
	entered := make(chan struct{})
	t.Cleanup(func() { close(wedged) })

	registry := acp.NewCapabilityRegistry()
	registry.RegisterMethod("session/update", func(json.RawMessage) (json.RawMessage, error) {
		close(entered)
		<-wedged // never returns while the test runs
		return nil, nil
	})

	connR, peerW := io.Pipe()
	c := acp.NewConn(connR, io.Discard, registry)
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- c.Serve(ctx) }()

	_, err := peerW.Write([]byte(`{"jsonrpc":"2.0","method":"session/update"}` + "\n"))
	require.NoError(t, err)
	<-entered // the handler is now wedged

	// A caller is waiting on the peer when the connection is torn down.
	errs := make(chan error, 1)
	go func() {
		_, err := c.Request(context.Background(), "session/request_permission", nil)
		errs <- err
	}()
	time.Sleep(50 * time.Millisecond)

	cancel()

	select {
	case <-served:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve blocked on a wedged notification handler")
	}

	select {
	case err := <-errs:
		assert.ErrorIs(t, err, acp.ErrConnClosed,
			"a pending caller must be released even when a handler is stuck")
	case <-time.After(2 * time.Second):
		t.Fatal("pending Request was never released")
	}
}
