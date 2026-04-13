package ws

import (
	"context"
	"encoding/json"
	"net"
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

func TestHub_SessionEmitter(t *testing.T) {
	srv, addr := startTestServer(t, NoopAuth{})
	hub := srv.Hub()

	// Create session and connect a client.
	ss, err := hub.CreateSession(context.Background(), "emitter-test", SessionCreatePayload{}, 100)
	require.NoError(t, err)

	conn := dialWS(t, addr, "")
	resumePayload, _ := json.Marshal(SessionResumePayload{SessionID: ss.ID, LastSeq: 0})
	writeEnvelope(t, conn, Envelope{Type: TypeSessionResume, Timestamp: time.Now().UTC(), Payload: resumePayload})

	// Drain the session_info message.
	env := readEnvelope(t, conn)
	assert.Equal(t, TypeSessionInfo, env.Type)

	// Emit a session event via the session-scoped emitter.
	emitter := hub.SessionEmitter(ss.ID)
	emitter.Emit(session.NewTurnStartedEvent("test prompt", "test-model"))

	// Client should receive it.
	env = readEnvelope(t, conn)
	assert.Equal(t, TypeEvent, env.Type)
	assert.Equal(t, ss.ID, env.SessionID)
	assert.True(t, env.Seq > 0)
}

func TestHub_BroadcastTurnEvent(t *testing.T) {
	srv, addr := startTestServer(t, NoopAuth{})
	hub := srv.Hub()

	ss, err := hub.CreateSession(context.Background(), "broadcast-test", SessionCreatePayload{}, 100)
	require.NoError(t, err)

	// Connect two clients.
	conn1 := dialWS(t, addr, "")
	conn2 := dialWS(t, addr, "")

	for _, conn := range []net.Conn{conn1, conn2} {
		resumePayload, _ := json.Marshal(SessionResumePayload{SessionID: ss.ID, LastSeq: 0})
		writeEnvelope(t, conn, Envelope{Type: TypeSessionResume, Timestamp: time.Now().UTC(), Payload: resumePayload})
		readEnvelope(t, conn) // drain session_info
	}

	// Broadcast a turn event.
	hub.BroadcastTurnEvent(ss, agentsdk.TurnEvent{
		Type: "text_delta",
		Text: "hello world",
	})

	// Both clients should receive it.
	for _, conn := range []net.Conn{conn1, conn2} {
		env := readEnvelope(t, conn)
		assert.Equal(t, TypeTurnEvent, env.Type)
		assert.Equal(t, ss.ID, env.SessionID)

		var payload map[string]any
		require.NoError(t, env.ParsePayload(&payload))
		assert.Equal(t, "text_delta", payload["type"])
		assert.Equal(t, "hello world", payload["text"])
	}
}

func TestHub_UIRequestHandler(t *testing.T) {
	srv, addr := startTestServer(t, NoopAuth{})
	hub := srv.Hub()

	ss, err := hub.CreateSession(context.Background(), "ui-test", SessionCreatePayload{}, 100)
	require.NoError(t, err)

	conn := dialWS(t, addr, "")
	resumePayload, _ := json.Marshal(SessionResumePayload{SessionID: ss.ID, LastSeq: 0})
	writeEnvelope(t, conn, Envelope{Type: TypeSessionResume, Timestamp: time.Now().UTC(), Payload: resumePayload})
	readEnvelope(t, conn) // drain session_info

	handler := hub.SessionUIHandler(ss.ID)

	// Start the request in a goroutine.
	type result struct {
		resp agentsdk.UIResponse
		err  error
	}
	resultCh := make(chan result, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		resp, err := handler.Request(ctx, agentsdk.UIRequest{
			ID:    "req-1",
			Kind:  agentsdk.UIKindApproval,
			Title: "Approve test",
		})
		resultCh <- result{resp, err}
	}()

	// Client should receive the UI request.
	env := readEnvelope(t, conn)
	assert.Equal(t, TypeUIRequest, env.Type)

	// Send UI response.
	uiResp := agentsdk.UIResponse{RequestID: "req-1", ActionID: "allow"}
	respPayload, _ := json.Marshal(uiResp)
	writeEnvelope(t, conn, Envelope{
		Type:      TypeUIResponse,
		SessionID: ss.ID,
		Timestamp: time.Now().UTC(),
		Payload:   respPayload,
	})

	// Request should complete.
	select {
	case r := <-resultCh:
		require.NoError(t, r.err)
		assert.Equal(t, "req-1", r.resp.RequestID)
		assert.Equal(t, "allow", r.resp.ActionID)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for UI response")
	}
}

func TestHub_UIRequestHandler_Timeout(t *testing.T) {
	srv, addr := startTestServer(t, NoopAuth{})
	hub := srv.Hub()

	ss, err := hub.CreateSession(context.Background(), "ui-timeout", SessionCreatePayload{}, 100)
	require.NoError(t, err)

	conn := dialWS(t, addr, "")
	resumePayload, _ := json.Marshal(SessionResumePayload{SessionID: ss.ID, LastSeq: 0})
	writeEnvelope(t, conn, Envelope{Type: TypeSessionResume, Timestamp: time.Now().UTC(), Payload: resumePayload})
	readEnvelope(t, conn) // drain session_info

	handler := hub.SessionUIHandler(ss.ID)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = handler.Request(ctx, agentsdk.UIRequest{
		ID:    "req-timeout",
		Kind:  agentsdk.UIKindConfirm,
		Title: "Confirm test",
	})
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestHub_Close(t *testing.T) {
	hub := NewHub(HubConfig{ReconnectBufferSize: 10})

	closed := false
	hub.AddBridge(&mockBridge{closeFn: func() error {
		closed = true
		return nil
	}})

	require.NoError(t, hub.Close())
	assert.True(t, closed)
}

// mockBridge implements bridge.EventBridge for testing.
type mockBridge struct {
	publishFn func(ctx context.Context, sessionID string, env bridge.Envelope) error
	closeFn   func() error
}

func (b *mockBridge) Publish(ctx context.Context, sessionID string, env bridge.Envelope) error {
	if b.publishFn != nil {
		return b.publishFn(ctx, sessionID, env)
	}
	return nil
}

func (b *mockBridge) Subscribe(_ context.Context, _ string, _ func(bridge.Envelope)) error {
	return nil
}

func (b *mockBridge) Close() error {
	if b.closeFn != nil {
		return b.closeFn()
	}
	return nil
}

func TestHub_Bridge_ReceivesEvents(t *testing.T) {
	srv, addr := startTestServer(t, NoopAuth{})
	hub := srv.Hub()

	received := make(chan bridge.Envelope, 10)
	hub.AddBridge(&mockBridge{
		publishFn: func(_ context.Context, _ string, env bridge.Envelope) error {
			received <- env
			return nil
		},
	})

	ss, err := hub.CreateSession(context.Background(), "bridge-test", SessionCreatePayload{}, 100)
	require.NoError(t, err)

	// Connect a client so broadcastToSession has recipients.
	conn := dialWS(t, addr, "")
	resumePayload, _ := json.Marshal(SessionResumePayload{SessionID: ss.ID, LastSeq: 0})
	writeEnvelope(t, conn, Envelope{Type: TypeSessionResume, Timestamp: time.Now().UTC(), Payload: resumePayload})
	readEnvelope(t, conn) // drain session_info

	// Broadcast a turn event.
	hub.BroadcastTurnEvent(ss, agentsdk.TurnEvent{
		Type: "done",
	})

	// Bridge should have received the event.
	select {
	case benv := <-received:
		assert.Equal(t, TypeTurnEvent, benv.Type)
		assert.Equal(t, "bridge-test", benv.SessionID)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for bridge event")
	}

	// Verify WebSocket client also received it.
	_ = readEnvelope(t, conn)
}

func TestHub_MarshalTurnEvent_WithError(t *testing.T) {
	raw, err := marshalTurnEvent(agentsdk.TurnEvent{
		Type:  "error",
		Error: assert.AnError,
	})
	require.NoError(t, err)

	var wire wireTurnEvent
	require.NoError(t, json.Unmarshal(raw, &wire))
	assert.Equal(t, "error", wire.Type)
	assert.Contains(t, wire.Error, "assert.AnError")
}

func TestHub_MarshalTurnEvent_DoneCarriesExitReason(t *testing.T) {
	// A done TurnEvent must serialize its ExitReason as a stable
	// lowercase string ("cancelled", "provider_error", ...) so
	// cross-language wire consumers can switch on it without pinning
	// the Go enum's integer value.
	raw, err := marshalTurnEvent(agentsdk.TurnEvent{
		Type:       "done",
		ExitReason: agentsdk.ExitProviderError,
	})
	require.NoError(t, err)

	var wire wireTurnEvent
	require.NoError(t, json.Unmarshal(raw, &wire))
	assert.Equal(t, "done", wire.Type)
	assert.Equal(t, "provider_error", wire.ExitReason)
}

func TestHub_MarshalTurnEvent_NonDoneOmitsExitReason(t *testing.T) {
	// Non-done events should omit ExitReason from the wire entirely
	// (omitempty) so consumers don't see a spurious "unknown" string
	// on every text_delta.
	raw, err := marshalTurnEvent(agentsdk.TurnEvent{
		Type: "text_delta",
		Text: "hello",
	})
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "exit_reason")
}

func TestHub_MarshalTurnEvent_WithToolProgress(t *testing.T) {
	raw, err := marshalTurnEvent(agentsdk.TurnEvent{
		Type: "tool_progress",
		ToolProgress: &agentsdk.ToolProgressEvent{
			ID:      "t1",
			Name:    "shell",
			Stage:   agentsdk.EventBegin,
			Content: "starting",
		},
	})
	require.NoError(t, err)

	var wire wireTurnEvent
	require.NoError(t, json.Unmarshal(raw, &wire))
	assert.Equal(t, "tool_progress", wire.Type)
	require.NotNil(t, wire.ToolProgress)
	assert.Equal(t, "begin", wire.ToolProgress.Stage)
	assert.Equal(t, "starting", wire.ToolProgress.Content)
}

// Test that wsutil is correctly used for server-side reads and client-side writes.
func TestClient_ReadPump_BinaryIgnored(t *testing.T) {
	srv, addr := startTestServer(t, NoopAuth{})
	_ = srv

	conn := dialWS(t, addr, "")

	// Send binary frame — should be ignored by readPump.
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	require.NoError(t, wsutil.WriteClientMessage(conn, ws.OpBinary, []byte{0x00, 0x01}))

	// Send a ping to verify connection is still alive.
	writeEnvelope(t, conn, Envelope{Type: TypePing, Timestamp: time.Now().UTC()})
	env := readEnvelope(t, conn)
	assert.Equal(t, TypePong, env.Type)
}
