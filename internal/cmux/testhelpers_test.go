package cmux_test

import (
	"encoding/json"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

// unmarshalParams decodes a jsonrpcRequest's Params into dst.
func unmarshalParams(req jsonrpcRequest, dst any) error {
	return json.Unmarshal(req.Params, dst)
}

// newErrorServer creates a fake cmux server that accepts Dial (ping+identify)
// but returns OK:false for errorMethod. All other methods return OK:true with
// an empty result.
func newErrorServer(t *testing.T, errorMethod string) string {
	t.Helper()
	socketPath := t.TempDir() + "/cmux_err.sock"
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
					switch {
					case req.Method == "system.ping":
						resultBytes, _ := json.Marshal(map[string]string{"pong": "ok"})
						_ = enc.Encode(map[string]interface{}{"id": req.ID, "ok": true, "result": json.RawMessage(resultBytes)})
					case req.Method == "system.identify":
						resultBytes, _ := json.Marshal(map[string]string{"window_id": "w", "workspace_id": "ws", "pane_id": "p", "surface_id": "s"})
						_ = enc.Encode(map[string]interface{}{"id": req.ID, "ok": true, "result": json.RawMessage(resultBytes)})
					case req.Method == errorMethod:
						_ = enc.Encode(map[string]interface{}{"id": req.ID, "ok": false, "error": errorMethod + " failed"})
					default:
						resultBytes, _ := json.Marshal(map[string]interface{}{})
						_ = enc.Encode(map[string]interface{}{"id": req.ID, "ok": true, "result": json.RawMessage(resultBytes)})
					}
				}
			}(conn)
		}
	}()
	return socketPath
}
