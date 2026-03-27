package ws

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startTestServer creates a server on a random port and returns the address.
func startTestServer(t *testing.T, auth Authenticator) (*Server, string) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	s := NewServer(ServerConfig{Auth: auth})
	go s.Serve(ln)

	t.Cleanup(func() {
		s.Shutdown(context.Background())
	})

	return s, ln.Addr().String()
}

// dialWS connects a WebSocket client to the server.
func dialWS(t *testing.T, addr, token string) net.Conn {
	t.Helper()

	url := "ws://" + addr + "/ws"
	if token != "" {
		url += "?token=" + token
	}

	conn, _, _, err := ws.Dial(context.Background(), url)
	require.NoError(t, err)

	t.Cleanup(func() { conn.Close() })
	return conn
}

// readEnvelope reads and decodes a single envelope from the connection.
func readEnvelope(t *testing.T, conn net.Conn) Envelope {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	msg, _, err := wsutil.ReadServerData(conn)
	require.NoError(t, err)

	var env Envelope
	require.NoError(t, json.Unmarshal(msg, &env))
	return env
}

// writeEnvelope encodes and sends an envelope.
func writeEnvelope(t *testing.T, conn net.Conn, env Envelope) {
	t.Helper()
	data, err := json.Marshal(env)
	require.NoError(t, err)
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	require.NoError(t, wsutil.WriteClientMessage(conn, ws.OpText, data))
}

func TestServer_HealthEndpoint(t *testing.T) {
	_, addr := startTestServer(t, NoopAuth{})

	resp, err := http.Get("http://" + addr + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestServer_WebSocketConnect_NoopAuth(t *testing.T) {
	_, addr := startTestServer(t, NoopAuth{})
	conn := dialWS(t, addr, "")

	// Send a ping and expect a pong.
	writeEnvelope(t, conn, Envelope{Type: TypePing, Timestamp: time.Now().UTC()})
	env := readEnvelope(t, conn)
	assert.Equal(t, TypePong, env.Type)
}

func TestServer_WebSocketConnect_StaticAuth_Success(t *testing.T) {
	_, addr := startTestServer(t, StaticTokenAuth{Token: "secret"})
	conn := dialWS(t, addr, "secret")

	writeEnvelope(t, conn, Envelope{Type: TypePing, Timestamp: time.Now().UTC()})
	env := readEnvelope(t, conn)
	assert.Equal(t, TypePong, env.Type)
}

func TestServer_WebSocketConnect_StaticAuth_Failure(t *testing.T) {
	_, addr := startTestServer(t, StaticTokenAuth{Token: "secret"})

	url := "ws://" + addr + "/ws?token=wrong"
	_, _, _, err := ws.Dial(context.Background(), url)
	assert.Error(t, err)
}

func TestServer_SessionCreate_And_List(t *testing.T) {
	_, addr := startTestServer(t, NoopAuth{})
	conn := dialWS(t, addr, "")

	// Create a session.
	payload, _ := json.Marshal(SessionCreatePayload{Model: "test-model"})
	writeEnvelope(t, conn, Envelope{
		Type:      TypeSessionCreate,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	})

	// Should receive session_info.
	env := readEnvelope(t, conn)
	assert.Equal(t, TypeSessionInfo, env.Type)

	var info SessionInfoPayload
	require.NoError(t, env.ParsePayload(&info))
	assert.NotEmpty(t, info.SessionID)
	assert.Equal(t, "active", info.Status)

	// List sessions.
	writeEnvelope(t, conn, Envelope{Type: TypeSessionList, Timestamp: time.Now().UTC()})
	env = readEnvelope(t, conn)
	assert.Equal(t, TypeSessionListResult, env.Type)
}

func TestServer_SessionResume_WithReplay(t *testing.T) {
	srv, addr := startTestServer(t, NoopAuth{})
	hub := srv.Hub()

	// Create a session and push some buffered events.
	ss, err := hub.CreateSession(context.Background(), "test-sess", SessionCreatePayload{}, 100)
	require.NoError(t, err)

	for i := int64(1); i <= 5; i++ {
		seq := ss.seq.Add(1)
		env := Envelope{
			Type:      TypeTurnEvent,
			SessionID: ss.ID,
			Seq:       seq,
			Timestamp: time.Now().UTC(),
			Payload:   json.RawMessage(`{"type":"text_delta","text":"chunk"}`),
		}
		data, _ := json.Marshal(env)
		ss.Buffer.Push(BufferEntry{Seq: seq, Payload: data})
	}

	// Connect and resume from seq 3.
	conn := dialWS(t, addr, "")

	resumePayload, _ := json.Marshal(SessionResumePayload{SessionID: "test-sess", LastSeq: 3})
	writeEnvelope(t, conn, Envelope{
		Type:      TypeSessionResume,
		Timestamp: time.Now().UTC(),
		Payload:   resumePayload,
	})

	// First message: session_info.
	env := readEnvelope(t, conn)
	assert.Equal(t, TypeSessionInfo, env.Type)

	// Next 2 messages: replayed events (seq 4, 5).
	for i := 0; i < 2; i++ {
		env = readEnvelope(t, conn)
		assert.Equal(t, TypeTurnEvent, env.Type)
	}
}

func TestServer_UnknownMessageType(t *testing.T) {
	_, addr := startTestServer(t, NoopAuth{})
	conn := dialWS(t, addr, "")

	writeEnvelope(t, conn, Envelope{
		Type:      "bogus_type",
		Timestamp: time.Now().UTC(),
	})

	env := readEnvelope(t, conn)
	assert.Equal(t, TypeError, env.Type)

	var errP ErrorPayload
	require.NoError(t, env.ParsePayload(&errP))
	assert.Equal(t, "unknown_type", errP.Code)
}

func TestServer_InvalidJSON(t *testing.T) {
	_, addr := startTestServer(t, NoopAuth{})
	conn := dialWS(t, addr, "")

	// Send garbage.
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	require.NoError(t, wsutil.WriteClientMessage(conn, ws.OpText, []byte("not json")))

	env := readEnvelope(t, conn)
	assert.Equal(t, TypeError, env.Type)

	var errP ErrorPayload
	require.NoError(t, env.ParsePayload(&errP))
	assert.Equal(t, "invalid_json", errP.Code)
}

func TestServer_UserMessage_NoSession(t *testing.T) {
	_, addr := startTestServer(t, NoopAuth{})
	conn := dialWS(t, addr, "")

	payload, _ := json.Marshal(UserMessagePayload{Text: "hello"})
	writeEnvelope(t, conn, Envelope{
		Type:      TypeUserMessage,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	})

	env := readEnvelope(t, conn)
	assert.Equal(t, TypeError, env.Type)

	var errP ErrorPayload
	require.NoError(t, env.ParsePayload(&errP))
	assert.Equal(t, "no_session", errP.Code)
}

func TestServer_Cancel_NoSession(t *testing.T) {
	// Cancel on a client with no session should not panic.
	_, addr := startTestServer(t, NoopAuth{})
	conn := dialWS(t, addr, "")

	writeEnvelope(t, conn, Envelope{Type: TypeCancel, Timestamp: time.Now().UTC()})

	// Send ping to verify connection is still alive.
	writeEnvelope(t, conn, Envelope{Type: TypePing, Timestamp: time.Now().UTC()})
	env := readEnvelope(t, conn)
	assert.Equal(t, TypePong, env.Type)
}
