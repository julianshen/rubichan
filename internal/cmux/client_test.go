package cmux_test

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/cmux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// jsonrpcRequest is the shape the client sends over the wire.
type jsonrpcRequest struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// handlerFunc returns a raw JSON payload for a given request.
type handlerFunc func(req jsonrpcRequest) interface{}

// fakeServer accepts connections on ln and serves canned responses from handlers.
// handlers maps method name → handlerFunc. Unrecognised methods return ok:false.
func fakeServer(t *testing.T, ln net.Listener, handlers map[string]handlerFunc) {
	t.Helper()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			go serveConn(t, conn, handlers)
		}
	}()
}

func serveConn(t *testing.T, conn net.Conn, handlers map[string]handlerFunc) {
	t.Helper()
	defer conn.Close()
	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)
	for {
		var req jsonrpcRequest
		if err := dec.Decode(&req); err != nil {
			return
		}
		h, ok := handlers[req.Method]
		if !ok {
			_ = enc.Encode(map[string]interface{}{
				"id":    req.ID,
				"ok":    false,
				"error": "unknown method: " + req.Method,
			})
			continue
		}
		result := h(req)
		resultBytes, _ := json.Marshal(result)
		_ = enc.Encode(map[string]interface{}{
			"id":     req.ID,
			"ok":     true,
			"result": json.RawMessage(resultBytes),
		})
	}
}

// newTestServer creates a temp Unix socket, starts fakeServer, and returns the socket path.
func newTestServer(t *testing.T, handlers map[string]handlerFunc) string {
	t.Helper()
	socketPath := filepath.Join(t.TempDir(), "cmux_test.sock")
	ln, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	fakeServer(t, ln, handlers)
	return socketPath
}

// defaultHandlers returns handlers that satisfy Dial (ping + identify).
func defaultHandlers() map[string]handlerFunc {
	return map[string]handlerFunc{
		"system.ping": func(req jsonrpcRequest) interface{} {
			return map[string]string{"pong": "ok"}
		},
		"system.identify": func(req jsonrpcRequest) interface{} {
			return map[string]string{
				"window_id":    "win-1",
				"workspace_id": "ws-42",
				"pane_id":      "pane-7",
				"surface_id":   "surf-3",
			}
		},
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestDial_Ping(t *testing.T) {
	socketPath := newTestServer(t, defaultHandlers())

	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	id := c.Identity()
	require.NotNil(t, id)
	assert.Equal(t, "win-1", id.WindowID)
	assert.Equal(t, "ws-42", id.WorkspaceID)
	assert.Equal(t, "pane-7", id.PaneID)
	assert.Equal(t, "surf-3", id.SurfaceID)
}

func TestCall_ErrorResponse(t *testing.T) {
	handlers := defaultHandlers()
	handlers["workspace.list"] = func(req jsonrpcRequest) interface{} {
		// Return an error response by using a raw encoder override.
		// We can't return ok:false from a handlerFunc that maps to ok:true,
		// so we use a special sentinel that the test server turns into ok:false.
		return nil // will be marshalled as null — but we need to test the error path
	}
	// Override serveConn logic by injecting an error-returning handler.
	socketPath := filepath.Join(t.TempDir(), "cmux_error.sock")
	ln, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	// Start a server that always returns ok:false for workspace.list.
	go func() {
		for {
			conn, err2 := ln.Accept()
			if err2 != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				dec := json.NewDecoder(c)
				enc := json.NewEncoder(c)
				for {
					var req jsonrpcRequest
					if err3 := dec.Decode(&req); err3 != nil {
						return
					}
					switch req.Method {
					case "system.ping":
						resultBytes, _ := json.Marshal(map[string]string{"pong": "ok"})
						_ = enc.Encode(map[string]interface{}{"id": req.ID, "ok": true, "result": json.RawMessage(resultBytes)})
					case "system.identify":
						resultBytes, _ := json.Marshal(map[string]string{"window_id": "w1", "workspace_id": "ws1", "pane_id": "p1", "surface_id": "s1"})
						_ = enc.Encode(map[string]interface{}{"id": req.ID, "ok": true, "result": json.RawMessage(resultBytes)})
					default:
						_ = enc.Encode(map[string]interface{}{"id": req.ID, "ok": false, "error": "permission denied"})
					}
				}
			}(conn)
		}
	}()

	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	resp, err := c.Call("workspace.list", map[string]string{})
	require.NoError(t, err) // Call itself succeeded (transport-wise)
	assert.False(t, resp.OK)
	assert.Equal(t, "permission denied", resp.Error)
}

func TestSocketPath_Default(t *testing.T) {
	t.Setenv("CMUX_SOCKET_PATH", "")
	assert.Equal(t, "/tmp/cmux.sock", cmux.SocketPath())
}

func TestSocketPath_Override(t *testing.T) {
	t.Setenv("CMUX_SOCKET_PATH", "/var/run/mycmux.sock")
	assert.Equal(t, "/var/run/mycmux.sock", cmux.SocketPath())
}

func TestDial_BadSocket(t *testing.T) {
	_, err := cmux.Dial("/tmp/nonexistent-cmux-test-socket-xyz.sock")
	require.Error(t, err)
}

// TestDial_PingRejected verifies Dial returns an error when system.ping returns OK:false.
func TestDial_PingRejected(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "cmux_ping_rej.sock")
	ln, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err2 := ln.Accept()
			if err2 != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				dec := json.NewDecoder(c)
				enc := json.NewEncoder(c)
				for {
					var req jsonrpcRequest
					if err3 := dec.Decode(&req); err3 != nil {
						return
					}
					_ = enc.Encode(map[string]interface{}{"id": req.ID, "ok": false, "error": "ping rejected"})
				}
			}(conn)
		}
	}()

	_, err = cmux.Dial(socketPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ping rejected")
}

// TestDial_PingFails verifies Dial returns an error when system.ping fails.
func TestDial_PingFails(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "cmux_ping_fail.sock")
	ln, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	// Server that closes the connection immediately on any request.
	go func() {
		for {
			conn, err2 := ln.Accept()
			if err2 != nil {
				return
			}
			conn.Close() // close before sending anything
		}
	}()

	_, err = cmux.Dial(socketPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ping failed")
}

// TestDial_IdentifyFails verifies Dial returns an error when system.identify fails.
func TestDial_IdentifyFails(t *testing.T) {
	socketPath := filepath.Join(os.TempDir(), fmt.Sprintf("cmux_idf_%d.sock", os.Getpid()))
	t.Cleanup(func() { os.Remove(socketPath) })
	ln, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err2 := ln.Accept()
			if err2 != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				dec := json.NewDecoder(c)
				enc := json.NewEncoder(c)
				for {
					var req jsonrpcRequest
					if err3 := dec.Decode(&req); err3 != nil {
						return
					}
					if req.Method == "system.ping" {
						resultBytes, _ := json.Marshal(map[string]string{"pong": "ok"})
						_ = enc.Encode(map[string]interface{}{"id": req.ID, "ok": true, "result": json.RawMessage(resultBytes)})
					} else {
						// identify: return ok:false
						_ = enc.Encode(map[string]interface{}{"id": req.ID, "ok": false, "error": "identify not allowed"})
					}
				}
			}(conn)
		}
	}()

	_, err = cmux.Dial(socketPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "identify failed")
}

// TestDial_IdentifyTransportError verifies Dial returns "identify failed" when the
// identify call fails at the transport level (server closes connection mid-stream).
func TestDial_IdentifyTransportError(t *testing.T) {
	socketPath := filepath.Join(os.TempDir(), fmt.Sprintf("cmux_ite_%d.sock", os.Getpid()))
	t.Cleanup(func() { os.Remove(socketPath) })
	ln, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err2 := ln.Accept()
			if err2 != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				dec := json.NewDecoder(c)
				enc := json.NewEncoder(c)
				var reqCount int
				for {
					var req jsonrpcRequest
					if err3 := dec.Decode(&req); err3 != nil {
						return
					}
					reqCount++
					if reqCount == 1 {
						// Answer ping.
						resultBytes, _ := json.Marshal(map[string]string{"pong": "ok"})
						_ = enc.Encode(map[string]interface{}{"id": req.ID, "ok": true, "result": json.RawMessage(resultBytes)})
					} else {
						// Close without answering identify.
						return
					}
				}
			}(conn)
		}
	}()

	_, err = cmux.Dial(socketPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "identify failed")
}

// TestDial_IdentifyBadJSON verifies Dial returns a decode error when identity
// result contains truly unparseable JSON.
func TestDial_IdentifyBadJSON(t *testing.T) {
	socketPath := filepath.Join(os.TempDir(), fmt.Sprintf("cmux_idj_%d.sock", os.Getpid()))
	t.Cleanup(func() { os.Remove(socketPath) })
	ln, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err2 := ln.Accept()
			if err2 != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				dec := json.NewDecoder(c)
				enc := json.NewEncoder(c)
				for {
					var req jsonrpcRequest
					if err3 := dec.Decode(&req); err3 != nil {
						return
					}
					switch req.Method {
					case "system.ping":
						resultBytes, _ := json.Marshal(map[string]string{"pong": "ok"})
						_ = enc.Encode(map[string]interface{}{"id": req.ID, "ok": true, "result": json.RawMessage(resultBytes)})
					case "system.identify":
						// Send truly invalid JSON as the result field value.
						// We write the framing manually to inject invalid bytes.
						raw := fmt.Sprintf(`{"id":%q,"ok":true,"result":{"window_id":}}`+"\n", req.ID)
						_, _ = c.Write([]byte(raw))
					}
				}
			}(conn)
		}
	}()

	_, err = cmux.Dial(socketPath)
	require.Error(t, err)
	// The invalid JSON causes Decode to fail at the transport level — so we get
	// "identify failed" (Call returns an error) rather than "decode identity".
	assert.Contains(t, err.Error(), "identify failed")
}

// TestCall_AfterClose verifies that Call returns an error when the connection is closed.
func TestCall_AfterClose(t *testing.T) {
	socketPath := newTestServer(t, defaultHandlers())

	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)

	err = c.Close()
	require.NoError(t, err)

	_, err = c.Call("system.ping", map[string]string{})
	require.Error(t, err)
}
