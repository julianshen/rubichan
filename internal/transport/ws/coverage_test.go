package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/julianshen/rubichan/internal/session"
	"github.com/julianshen/rubichan/internal/transport/bridge"
	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProvider implements agentsdk.LLMProvider for Hub tests.
type fakeProvider struct {
	events []agentsdk.StreamEvent
}

func (p *fakeProvider) Stream(_ context.Context, _ agentsdk.CompletionRequest) (<-chan agentsdk.StreamEvent, error) {
	ch := make(chan agentsdk.StreamEvent, len(p.events)+1)
	for _, ev := range p.events {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

// failingFactory returns an error, exercising CreateSession's error branch.
func failingFactory(_ string, _ SessionCreatePayload) (*agentsdk.Agent, error) {
	return nil, errors.New("factory boom")
}

// simpleAgentFactory creates a no-op agent suitable for handleUserMessage tests.
func simpleAgentFactory(_ string, _ SessionCreatePayload) (*agentsdk.Agent, error) {
	p := &fakeProvider{events: []agentsdk.StreamEvent{
		{Type: "text_delta", Text: "ok"},
		{Type: "stop", InputTokens: 1, OutputTokens: 1},
	}}
	return agentsdk.NewAgent(p), nil
}

// startTestServerWithFactory creates a server wired with an agent factory.
func startTestServerWithFactory(t *testing.T, factory AgentFactory) (*Server, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	s := NewServer(ServerConfig{Auth: NoopAuth{}, AgentFactory: factory})
	go s.Serve(ln)
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })
	return s, ln.Addr().String()
}

// -------- CreateSession --------

// TestCreateSession_FactoryError covers the error-returning path.
func TestCreateSession_FactoryError(t *testing.T) {
	t.Parallel()
	hub := NewHub(HubConfig{AgentFactory: failingFactory})
	_, err := hub.CreateSession(context.Background(), "fail", SessionCreatePayload{}, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "factory boom")
}

// TestCreateSession_DefaultBufferSize covers the defaults branch when buffer
// is zero and hub bufferCap is also zero.
func TestCreateSession_DefaultBufferSize(t *testing.T) {
	t.Parallel()
	hub := NewHub(HubConfig{}) // triggers ReconnectBufferSize default in NewHub
	ss, err := hub.CreateSession(context.Background(), "s", SessionCreatePayload{}, 0)
	require.NoError(t, err)
	require.NotNil(t, ss.Buffer)
}

// -------- Emit (session.EventSink) --------

// TestHub_Emit_AllSessions broadcasts an event with no SessionID to all
// active sessions and verifies clients connected to each receive it.
func TestHub_Emit_AllSessions(t *testing.T) {
	srv, addr := startTestServer(t, NoopAuth{})
	hub := srv.Hub()

	ss1, err := hub.CreateSession(context.Background(), "emit1", SessionCreatePayload{}, 10)
	require.NoError(t, err)
	ss2, err := hub.CreateSession(context.Background(), "emit2", SessionCreatePayload{}, 10)
	require.NoError(t, err)

	conn1 := dialWS(t, addr, "")
	conn2 := dialWS(t, addr, "")

	for i, conn := range []net.Conn{conn1, conn2} {
		id := ss1.ID
		if i == 1 {
			id = ss2.ID
		}
		resumePayload, _ := json.Marshal(SessionResumePayload{SessionID: id, LastSeq: 0})
		writeEnvelope(t, conn, Envelope{Type: TypeSessionResume, Timestamp: time.Now().UTC(), Payload: resumePayload})
		readEnvelope(t, conn) // drain session_info
	}

	hub.Emit(session.Event{Type: "test-broadcast"})

	for _, conn := range []net.Conn{conn1, conn2} {
		env := readEnvelope(t, conn)
		assert.Equal(t, TypeEvent, env.Type)
	}
}

// TestHub_Emit_TargetedSession verifies that events with a SessionID route
// only to that session.
func TestHub_Emit_TargetedSession(t *testing.T) {
	srv, addr := startTestServer(t, NoopAuth{})
	hub := srv.Hub()

	ss, err := hub.CreateSession(context.Background(), "target", SessionCreatePayload{}, 10)
	require.NoError(t, err)

	conn := dialWS(t, addr, "")
	resumePayload, _ := json.Marshal(SessionResumePayload{SessionID: ss.ID, LastSeq: 0})
	writeEnvelope(t, conn, Envelope{Type: TypeSessionResume, Timestamp: time.Now().UTC(), Payload: resumePayload})
	readEnvelope(t, conn)

	hub.Emit(session.Event{Type: "directed", SessionID: ss.ID})

	env := readEnvelope(t, conn)
	assert.Equal(t, TypeEvent, env.Type)
	assert.Equal(t, ss.ID, env.SessionID)
}

// TestHub_Emit_UnknownSession covers the "session not found" log branch.
// We just make sure Emit doesn't panic or crash.
func TestHub_Emit_UnknownSession(t *testing.T) {
	t.Parallel()
	hub := NewHub(HubConfig{})
	// Should not panic and should log+drop.
	hub.Emit(session.Event{Type: "x", SessionID: "ghost"})
}

// -------- ContextWithSessionID + Request --------

// TestRequest_WithContextSessionID routes a UIRequest via ContextWithSessionID.
func TestRequest_WithContextSessionID(t *testing.T) {
	srv, addr := startTestServer(t, NoopAuth{})
	hub := srv.Hub()

	ss, err := hub.CreateSession(context.Background(), "req-ctx", SessionCreatePayload{}, 10)
	require.NoError(t, err)

	conn := dialWS(t, addr, "")
	resumePayload, _ := json.Marshal(SessionResumePayload{SessionID: ss.ID, LastSeq: 0})
	writeEnvelope(t, conn, Envelope{Type: TypeSessionResume, Timestamp: time.Now().UTC(), Payload: resumePayload})
	readEnvelope(t, conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = ContextWithSessionID(ctx, ss.ID)

	type result struct {
		resp agentsdk.UIResponse
		err  error
	}
	resultCh := make(chan result, 1)
	go func() {
		resp, err := hub.Request(ctx, agentsdk.UIRequest{ID: "rq1", Kind: agentsdk.UIKindConfirm, Title: "?"})
		resultCh <- result{resp, err}
	}()

	env := readEnvelope(t, conn)
	assert.Equal(t, TypeUIRequest, env.Type)

	respPayload, _ := json.Marshal(agentsdk.UIResponse{RequestID: "rq1", ActionID: "ok"})
	writeEnvelope(t, conn, Envelope{Type: TypeUIResponse, SessionID: ss.ID, Timestamp: time.Now().UTC(), Payload: respPayload})

	select {
	case r := <-resultCh:
		require.NoError(t, r.err)
		assert.Equal(t, "ok", r.resp.ActionID)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

// TestRequest_ContextSessionIDNotFound covers the "session not found" error.
func TestRequest_ContextSessionIDNotFound(t *testing.T) {
	t.Parallel()
	hub := NewHub(HubConfig{})
	ctx := ContextWithSessionID(context.Background(), "ghost")
	_, err := hub.Request(ctx, agentsdk.UIRequest{ID: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestRequest_NoConnectedClients covers the fallback with zero sessions.
func TestRequest_NoConnectedClients(t *testing.T) {
	t.Parallel()
	hub := NewHub(HubConfig{})
	_, err := hub.Request(context.Background(), agentsdk.UIRequest{ID: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no connected clients")
}

// TestRequest_FallbackSingleSession covers the fallback branch where Request
// finds exactly one session with clients and routes to it.
func TestRequest_FallbackSingleSession(t *testing.T) {
	srv, addr := startTestServer(t, NoopAuth{})
	hub := srv.Hub()

	ss, err := hub.CreateSession(context.Background(), "fallback", SessionCreatePayload{}, 10)
	require.NoError(t, err)

	conn := dialWS(t, addr, "")
	resumePayload, _ := json.Marshal(SessionResumePayload{SessionID: ss.ID, LastSeq: 0})
	writeEnvelope(t, conn, Envelope{Type: TypeSessionResume, Timestamp: time.Now().UTC(), Payload: resumePayload})
	readEnvelope(t, conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	type result struct {
		resp agentsdk.UIResponse
		err  error
	}
	resultCh := make(chan result, 1)
	go func() {
		resp, err := hub.Request(ctx, agentsdk.UIRequest{ID: "fb1", Kind: agentsdk.UIKindApproval, Title: "?"})
		resultCh <- result{resp, err}
	}()

	env := readEnvelope(t, conn)
	assert.Equal(t, TypeUIRequest, env.Type)

	respPayload, _ := json.Marshal(agentsdk.UIResponse{RequestID: "fb1", ActionID: "allow"})
	writeEnvelope(t, conn, Envelope{Type: TypeUIResponse, SessionID: ss.ID, Timestamp: time.Now().UTC(), Payload: respPayload})

	select {
	case r := <-resultCh:
		require.NoError(t, r.err)
		assert.Equal(t, "allow", r.resp.ActionID)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

// TestRequest_MultipleActiveSessionsAmbiguous covers the ambiguous-fallback error.
func TestRequest_MultipleActiveSessionsAmbiguous(t *testing.T) {
	srv, addr := startTestServer(t, NoopAuth{})
	hub := srv.Hub()

	ss1, err := hub.CreateSession(context.Background(), "amb1", SessionCreatePayload{}, 10)
	require.NoError(t, err)
	ss2, err := hub.CreateSession(context.Background(), "amb2", SessionCreatePayload{}, 10)
	require.NoError(t, err)

	for _, id := range []string{ss1.ID, ss2.ID} {
		conn := dialWS(t, addr, "")
		resumePayload, _ := json.Marshal(SessionResumePayload{SessionID: id, LastSeq: 0})
		writeEnvelope(t, conn, Envelope{Type: TypeSessionResume, Timestamp: time.Now().UTC(), Payload: resumePayload})
		readEnvelope(t, conn)
	}

	_, err = hub.Request(context.Background(), agentsdk.UIRequest{ID: "rq"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple sessions active")
}

// -------- SessionUIHandler error path --------

// TestSessionUIHandler_UnknownSession covers the error branch.
func TestSessionUIHandler_UnknownSession(t *testing.T) {
	t.Parallel()
	hub := NewHub(HubConfig{})
	handler := hub.SessionUIHandler("missing")
	_, err := handler.Request(context.Background(), agentsdk.UIRequest{ID: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// -------- SessionEmitter error path --------

// TestSessionEmitter_UnknownSession covers the logging drop branch.
func TestSessionEmitter_UnknownSession(t *testing.T) {
	t.Parallel()
	hub := NewHub(HubConfig{})
	emitter := hub.SessionEmitter("missing")
	// Should not panic.
	emitter.Emit(session.Event{Type: "x"})
}

// -------- handleUserMessage success path --------

// TestHandleUserMessage_Success covers end-to-end user_message → agent.Turn.
func TestHandleUserMessage_Success(t *testing.T) {
	_, addr := startTestServerWithFactory(t, simpleAgentFactory)

	conn := dialWS(t, addr, "")

	// Create a session so we get an attached agent.
	createPayload, _ := json.Marshal(SessionCreatePayload{Model: "test"})
	writeEnvelope(t, conn, Envelope{Type: TypeSessionCreate, Timestamp: time.Now().UTC(), Payload: createPayload})

	env := readEnvelope(t, conn)
	require.Equal(t, TypeSessionInfo, env.Type)

	msgPayload, _ := json.Marshal(UserMessagePayload{Text: "hi"})
	writeEnvelope(t, conn, Envelope{Type: TypeUserMessage, Timestamp: time.Now().UTC(), Payload: msgPayload})

	// Consume turn events until we see "done" or timeout.
	deadline := time.Now().Add(5 * time.Second)
	sawDone := false
	for time.Now().Before(deadline) && !sawDone {
		env = readEnvelope(t, conn)
		if env.Type == TypeTurnEvent {
			var p map[string]any
			require.NoError(t, env.ParsePayload(&p))
			if p["type"] == "done" {
				sawDone = true
			}
		}
	}
	assert.True(t, sawDone, "expected to see a done turn event")
}

// TestHandleUserMessage_EmptyText covers the empty-text rejection.
func TestHandleUserMessage_EmptyText(t *testing.T) {
	_, addr := startTestServerWithFactory(t, simpleAgentFactory)
	conn := dialWS(t, addr, "")

	createPayload, _ := json.Marshal(SessionCreatePayload{})
	writeEnvelope(t, conn, Envelope{Type: TypeSessionCreate, Timestamp: time.Now().UTC(), Payload: createPayload})
	readEnvelope(t, conn) // session_info

	msgPayload, _ := json.Marshal(UserMessagePayload{Text: ""})
	writeEnvelope(t, conn, Envelope{Type: TypeUserMessage, Timestamp: time.Now().UTC(), Payload: msgPayload})

	env := readEnvelope(t, conn)
	assert.Equal(t, TypeError, env.Type)
	var errP ErrorPayload
	require.NoError(t, env.ParsePayload(&errP))
	assert.Equal(t, "empty_message", errP.Code)
}

// TestHandleUserMessage_InvalidPayload exercises the invalid-payload branch.
func TestHandleUserMessage_InvalidPayload(t *testing.T) {
	_, addr := startTestServerWithFactory(t, simpleAgentFactory)
	conn := dialWS(t, addr, "")

	createPayload, _ := json.Marshal(SessionCreatePayload{})
	writeEnvelope(t, conn, Envelope{Type: TypeSessionCreate, Timestamp: time.Now().UTC(), Payload: createPayload})
	readEnvelope(t, conn)

	// Invalid: a string where object is expected
	writeEnvelope(t, conn, Envelope{Type: TypeUserMessage, Timestamp: time.Now().UTC(), Payload: json.RawMessage(`"oops"`)})

	env := readEnvelope(t, conn)
	assert.Equal(t, TypeError, env.Type)
	var errP ErrorPayload
	require.NoError(t, env.ParsePayload(&errP))
	assert.Equal(t, "invalid_payload", errP.Code)
}

// TestHandleUserMessage_NoAgent covers the no-agent branch when server has no factory.
func TestHandleUserMessage_NoAgent(t *testing.T) {
	_, addr := startTestServer(t, NoopAuth{}) // no AgentFactory, so CreateSession leaves Agent nil
	conn := dialWS(t, addr, "")

	createPayload, _ := json.Marshal(SessionCreatePayload{})
	writeEnvelope(t, conn, Envelope{Type: TypeSessionCreate, Timestamp: time.Now().UTC(), Payload: createPayload})
	env := readEnvelope(t, conn)
	require.Equal(t, TypeSessionInfo, env.Type)

	msgPayload, _ := json.Marshal(UserMessagePayload{Text: "hi"})
	writeEnvelope(t, conn, Envelope{Type: TypeUserMessage, Timestamp: time.Now().UTC(), Payload: msgPayload})

	env = readEnvelope(t, conn)
	assert.Equal(t, TypeError, env.Type)
	var errP ErrorPayload
	require.NoError(t, env.ParsePayload(&errP))
	assert.Equal(t, "no_agent", errP.Code)
}

// -------- handleSessionCreate error paths --------

// TestHandleSessionCreate_InvalidPayload covers the invalid-payload branch.
func TestHandleSessionCreate_InvalidPayload(t *testing.T) {
	_, addr := startTestServer(t, NoopAuth{})
	conn := dialWS(t, addr, "")

	writeEnvelope(t, conn, Envelope{Type: TypeSessionCreate, Timestamp: time.Now().UTC(), Payload: json.RawMessage(`"bad"`)})
	env := readEnvelope(t, conn)
	assert.Equal(t, TypeError, env.Type)
	var errP ErrorPayload
	require.NoError(t, env.ParsePayload(&errP))
	assert.Equal(t, "invalid_payload", errP.Code)
}

// TestHandleSessionCreate_FactoryFailure exercises the create_failed branch.
func TestHandleSessionCreate_FactoryFailure(t *testing.T) {
	_, addr := startTestServerWithFactory(t, failingFactory)
	conn := dialWS(t, addr, "")

	createPayload, _ := json.Marshal(SessionCreatePayload{})
	writeEnvelope(t, conn, Envelope{Type: TypeSessionCreate, Timestamp: time.Now().UTC(), Payload: createPayload})

	env := readEnvelope(t, conn)
	assert.Equal(t, TypeError, env.Type)
	var errP ErrorPayload
	require.NoError(t, env.ParsePayload(&errP))
	assert.Equal(t, "create_failed", errP.Code)
}

// -------- handleSessionResume error paths --------

// TestHandleSessionResume_InvalidPayload covers the bad-JSON branch.
func TestHandleSessionResume_InvalidPayload(t *testing.T) {
	_, addr := startTestServer(t, NoopAuth{})
	conn := dialWS(t, addr, "")

	writeEnvelope(t, conn, Envelope{Type: TypeSessionResume, Timestamp: time.Now().UTC(), Payload: json.RawMessage(`"bad"`)})
	env := readEnvelope(t, conn)
	assert.Equal(t, TypeError, env.Type)
	var errP ErrorPayload
	require.NoError(t, env.ParsePayload(&errP))
	assert.Equal(t, "invalid_payload", errP.Code)
}

// TestHandleSessionResume_MissingSession covers the not-found branch.
func TestHandleSessionResume_MissingSession(t *testing.T) {
	_, addr := startTestServer(t, NoopAuth{})
	conn := dialWS(t, addr, "")

	resumePayload, _ := json.Marshal(SessionResumePayload{SessionID: "ghost"})
	writeEnvelope(t, conn, Envelope{Type: TypeSessionResume, Timestamp: time.Now().UTC(), Payload: resumePayload})

	env := readEnvelope(t, conn)
	assert.Equal(t, TypeError, env.Type)
	var errP ErrorPayload
	require.NoError(t, env.ParsePayload(&errP))
	assert.Equal(t, "resume_failed", errP.Code)
}

// -------- handleUIResponse orphan path --------

// TestHandleUIResponse_InvalidPayload covers the invalid-payload branch.
func TestHandleUIResponse_InvalidPayload(t *testing.T) {
	_, addr := startTestServer(t, NoopAuth{})
	conn := dialWS(t, addr, "")

	writeEnvelope(t, conn, Envelope{Type: TypeUIResponse, Timestamp: time.Now().UTC(), Payload: json.RawMessage(`"bad"`)})
	env := readEnvelope(t, conn)
	assert.Equal(t, TypeError, env.Type)
	var errP ErrorPayload
	require.NoError(t, env.ParsePayload(&errP))
	assert.Equal(t, "invalid_payload", errP.Code)
}

// TestHandleUIResponse_NoSession exercises the "no session" silent return.
// We send a valid UIResponse from a client not attached to any session,
// then verify the connection is still alive.
func TestHandleUIResponse_NoSession(t *testing.T) {
	_, addr := startTestServer(t, NoopAuth{})
	conn := dialWS(t, addr, "")

	uiResp := agentsdk.UIResponse{RequestID: "orphan", ActionID: "ok"}
	payload, _ := json.Marshal(uiResp)
	writeEnvelope(t, conn, Envelope{Type: TypeUIResponse, Timestamp: time.Now().UTC(), Payload: payload})

	// Follow up with a ping to confirm conn is alive.
	writeEnvelope(t, conn, Envelope{Type: TypePing, Timestamp: time.Now().UTC()})
	env := readEnvelope(t, conn)
	assert.Equal(t, TypePong, env.Type)
}

// TestHandleUIResponse_UnknownRequestID covers the "unknown request id" path.
func TestHandleUIResponse_UnknownRequestID(t *testing.T) {
	srv, addr := startTestServer(t, NoopAuth{})
	hub := srv.Hub()

	ss, err := hub.CreateSession(context.Background(), "unk", SessionCreatePayload{}, 10)
	require.NoError(t, err)

	conn := dialWS(t, addr, "")
	resumePayload, _ := json.Marshal(SessionResumePayload{SessionID: ss.ID, LastSeq: 0})
	writeEnvelope(t, conn, Envelope{Type: TypeSessionResume, Timestamp: time.Now().UTC(), Payload: resumePayload})
	readEnvelope(t, conn)

	uiResp := agentsdk.UIResponse{RequestID: "never-existed", ActionID: "ok"}
	payload, _ := json.Marshal(uiResp)
	writeEnvelope(t, conn, Envelope{Type: TypeUIResponse, SessionID: ss.ID, Timestamp: time.Now().UTC(), Payload: payload})

	// Connection still alive after silent drop.
	writeEnvelope(t, conn, Envelope{Type: TypePing, Timestamp: time.Now().UTC()})
	env := readEnvelope(t, conn)
	assert.Equal(t, TypePong, env.Type)
}

// -------- handleCancel: session attached, cancel called --------

// TestHandleCancel_WithAttachedSession covers the happy-path where cancel is called.
func TestHandleCancel_WithAttachedSession(t *testing.T) {
	_, addr := startTestServerWithFactory(t, simpleAgentFactory)
	conn := dialWS(t, addr, "")

	createPayload, _ := json.Marshal(SessionCreatePayload{})
	writeEnvelope(t, conn, Envelope{Type: TypeSessionCreate, Timestamp: time.Now().UTC(), Payload: createPayload})
	readEnvelope(t, conn) // session_info

	// Send a user_message so ss.cancel is populated.
	msgPayload, _ := json.Marshal(UserMessagePayload{Text: "hi"})
	writeEnvelope(t, conn, Envelope{Type: TypeUserMessage, Timestamp: time.Now().UTC(), Payload: msgPayload})

	// Immediately cancel — at least exercises the code paths.
	writeEnvelope(t, conn, Envelope{Type: TypeCancel, Timestamp: time.Now().UTC()})

	// Drain any remaining messages until ping/pong is observed.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		writeEnvelope(t, conn, Envelope{Type: TypePing, Timestamp: time.Now().UTC()})
		env := readEnvelope(t, conn)
		if env.Type == TypePong {
			return
		}
	}
}

// -------- snapshotAndRegisterClient reconnect-buffer-gap --------

// TestSnapshotAndRegisterClient_NotFound covers the session-not-found branch.
func TestSnapshotAndRegisterClient_NotFound(t *testing.T) {
	t.Parallel()
	hub := NewHub(HubConfig{})

	srvConn, _ := net.Pipe()
	defer srvConn.Close()
	c := newClient(hub, srvConn, AuthClaims{})

	_, err := hub.snapshotAndRegisterClient(c, "ghost", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// -------- Client.Send: closed / full buffer branches --------

// TestClient_Send_AfterDone covers the <-c.done early-return branch of Send.
func TestClient_Send_AfterDone(t *testing.T) {
	t.Parallel()
	srvConn, _ := net.Pipe()
	defer srvConn.Close()
	c := newClient(&Hub{}, srvConn, AuthClaims{})
	c.close()
	assert.False(t, c.Send([]byte("x")))
}

// TestClient_Send_BufferFull covers the buffer-full path that triggers
// close on the slow client.
func TestClient_Send_BufferFull(t *testing.T) {
	t.Parallel()
	srvConn, _ := net.Pipe()
	defer srvConn.Close()
	c := newClient(&Hub{}, srvConn, AuthClaims{})

	// Pre-fill the send buffer.
	for i := 0; i < sendBufferSize; i++ {
		select {
		case c.send <- []byte("fill"):
		default:
			t.Fatal("unexpected early full")
		}
	}
	// Next Send should hit the default branch and close the client.
	assert.False(t, c.Send([]byte("overflow")))

	// c.done must now be closed.
	select {
	case <-c.done:
	default:
		t.Fatal("expected client to be closed after buffer overflow")
	}
}

// -------- logBridgeError: ensure invocation is safe --------

// TestLogBridgeError just exercises the log line.
func TestLogBridgeError(t *testing.T) {
	t.Parallel()
	logBridgeError("s1", "turn_event", errors.New("publish boom"))
}

// -------- Bridge publish error path --------

// TestBridge_PublishError ensures broadcasting continues even when a bridge
// returns an error, exercising logBridgeError indirectly.
func TestBridge_PublishError(t *testing.T) {
	srv, addr := startTestServer(t, NoopAuth{})
	hub := srv.Hub()

	hub.AddBridge(&mockBridge{
		publishFn: func(_ context.Context, _ string, _ bridge.Envelope) error {
			return errors.New("publish failed")
		},
	})

	ss, err := hub.CreateSession(context.Background(), "pub-err", SessionCreatePayload{}, 10)
	require.NoError(t, err)

	conn := dialWS(t, addr, "")
	resumePayload, _ := json.Marshal(SessionResumePayload{SessionID: ss.ID, LastSeq: 0})
	writeEnvelope(t, conn, Envelope{Type: TypeSessionResume, Timestamp: time.Now().UTC(), Payload: resumePayload})
	readEnvelope(t, conn)

	hub.BroadcastTurnEvent(ss, agentsdk.TurnEvent{Type: "text_delta", Text: "x"})
	env := readEnvelope(t, conn)
	assert.Equal(t, TypeTurnEvent, env.Type)
}

// -------- writePump periodic ping --------

// TestClient_WritePump_PingTick triggers the writePump ping branch by writing
// a short ping period. Because pingPeriod is a const we cannot override it,
// but we can still cover writePump's normal send branch by relying on the main
// server path. This test simply ensures multiple messages flush without error.
func TestClient_WritePump_MultipleSends(t *testing.T) {
	_, addr := startTestServer(t, NoopAuth{})
	conn := dialWS(t, addr, "")

	for i := 0; i < 3; i++ {
		writeEnvelope(t, conn, Envelope{Type: TypePing, Timestamp: time.Now().UTC()})
		env := readEnvelope(t, conn)
		assert.Equal(t, TypePong, env.Type)
	}
}

// -------- Dead client in broadcastToSession --------

// TestBroadcastToSession_DeadClient exercises the "broadcast to clients" path
// where one of the clients has been closed — the slow-client close path runs
// without a deadlock. We use a full buffer to simulate a dead client.
func TestBroadcastToSession_DeadClient(t *testing.T) {
	t.Parallel()
	hub := NewHub(HubConfig{ReconnectBufferSize: 10})
	ss, err := hub.CreateSession(context.Background(), "dead", SessionCreatePayload{}, 10)
	require.NoError(t, err)

	srvConn, _ := net.Pipe()
	defer srvConn.Close()
	c := newClient(hub, srvConn, AuthClaims{})
	// Fill buffer so Send returns false immediately.
	for i := 0; i < sendBufferSize; i++ {
		c.send <- []byte("x")
	}
	ss.mu.Lock()
	ss.Clients[c] = struct{}{}
	ss.mu.Unlock()

	hub.BroadcastTurnEvent(ss, agentsdk.TurnEvent{Type: "text_delta", Text: "x"})
	// If we got here without deadlock, the test passes.
}

// -------- readPump OversizedFrame, server Close --------

// TestServer_CloseIdempotent ensures Close can be called twice without error.
func TestServer_CloseIdempotent(t *testing.T) {
	t.Parallel()
	hub := NewHub(HubConfig{})
	require.NoError(t, hub.Close())
	// Second close must not panic.
	require.NoError(t, hub.Close())
}

// TestReadPump_LargePayloadStillWorks ensures large but-legal messages work.
func TestReadPump_LargePayloadStillWorks(t *testing.T) {
	_, addr := startTestServer(t, NoopAuth{})
	conn := dialWS(t, addr, "")

	// ~1 KiB payload — well within the 1 MiB cap.
	text := string(make([]byte, 1024))
	payload, _ := json.Marshal(UserMessagePayload{Text: text})
	writeEnvelope(t, conn, Envelope{Type: TypeUserMessage, Timestamp: time.Now().UTC(), Payload: payload})

	env := readEnvelope(t, conn)
	assert.Equal(t, TypeError, env.Type)
	var errP ErrorPayload
	require.NoError(t, env.ParsePayload(&errP))
	assert.Equal(t, "no_session", errP.Code)
}

// TestReadPump_InvalidEnvelope covers Envelope.Validate's error path.
func TestReadPump_InvalidEnvelope(t *testing.T) {
	_, addr := startTestServer(t, NoopAuth{})
	conn := dialWS(t, addr, "")

	// Missing type field.
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	require.NoError(t, wsutil.WriteClientMessage(conn, ws.OpText, []byte(`{"timestamp":"2024-01-01T00:00:00Z"}`)))

	env := readEnvelope(t, conn)
	assert.Equal(t, TypeError, env.Type)
	var errP ErrorPayload
	require.NoError(t, env.ParsePayload(&errP))
	assert.Equal(t, "invalid_envelope", errP.Code)
}

// -------- concurrent CreateSession + Emit --------

// TestHub_ConcurrentCreateAndEmit stress-tests CreateSession and Emit together
// to surface any races via -race. Uses atomic iteration count via a waitgroup.
func TestHub_ConcurrentCreateAndEmit(t *testing.T) {
	t.Parallel()
	hub := NewHub(HubConfig{})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "s-" + time.Now().Format("150405.000000") + "-" + string(rune('a'+i))
			_, _ = hub.CreateSession(context.Background(), id, SessionCreatePayload{}, 5)
		}(i)
	}
	wg.Wait()

	// Emit to all to ensure no panic.
	hub.Emit(session.Event{Type: "test"})
}
