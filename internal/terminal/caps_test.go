package terminal

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectWithEnv_Ghostty(t *testing.T) {
	caps := DetectWithEnv("ghostty", nil)
	assert.True(t, caps.Hyperlinks)
	assert.True(t, caps.KittyGraphics)
	assert.True(t, caps.KittyKeyboard)
	assert.True(t, caps.ProgressBar)
	assert.True(t, caps.Notifications)
	assert.True(t, caps.SyncRendering)
	assert.True(t, caps.ClipboardAccess)
	assert.True(t, caps.FocusEvents)
}

func TestDetectWithEnv_Kitty(t *testing.T) {
	caps := DetectWithEnv("kitty", nil)
	assert.True(t, caps.Hyperlinks)
	assert.True(t, caps.KittyGraphics)
	assert.True(t, caps.KittyKeyboard)
	assert.False(t, caps.ProgressBar)
	assert.True(t, caps.Notifications)
	assert.True(t, caps.SyncRendering)
	assert.True(t, caps.ClipboardAccess)
	assert.True(t, caps.FocusEvents)
}

func TestDetectWithEnv_AppleTerminal(t *testing.T) {
	caps := DetectWithEnv("Apple_Terminal", nil)
	assert.True(t, caps.Hyperlinks)
	assert.False(t, caps.KittyGraphics)
	assert.False(t, caps.KittyKeyboard)
	assert.False(t, caps.ProgressBar)
	assert.False(t, caps.Notifications)
	assert.False(t, caps.SyncRendering)
	assert.False(t, caps.ClipboardAccess)
	assert.False(t, caps.FocusEvents)
}

func TestDetectWithEnv_UnknownTerminal(t *testing.T) {
	caps := DetectWithEnv("unknown-terminal", nil)
	assert.False(t, caps.Hyperlinks)
	assert.False(t, caps.KittyGraphics)
	assert.False(t, caps.ProgressBar)
}

func TestDetectWithEnv_EmptyTermProgram(t *testing.T) {
	caps := DetectWithEnv("", nil)
	assert.False(t, caps.Hyperlinks)
	assert.False(t, caps.KittyGraphics)
}

func TestDetectWithEnv_CmuxSocket_Detected(t *testing.T) {
	t.Setenv("CMUX_WORKSPACE_ID", "ws-123")
	// Create fake cmux socket that responds to system.ping.
	// Use os.MkdirTemp with a short prefix to stay within the Unix socket
	// path limit of 104 characters on macOS.
	dir, err := os.MkdirTemp("", "cmux")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "cmux.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		dec := json.NewDecoder(conn)
		enc := json.NewEncoder(conn)
		for {
			var req map[string]any
			if err := dec.Decode(&req); err != nil {
				return
			}
			enc.Encode(map[string]any{"id": req["id"], "ok": true, "result": map[string]bool{"pong": true}}) //nolint:errcheck
		}
	}()
	t.Setenv("CMUX_SOCKET_PATH", sock)
	caps := DetectWithEnv("ghostty", nil)
	assert.True(t, caps.CmuxSocket)
}

func TestDetectWithEnv_CmuxSocket_NoEnvVar(t *testing.T) {
	t.Setenv("CMUX_WORKSPACE_ID", "")
	caps := DetectWithEnv("ghostty", nil)
	assert.False(t, caps.CmuxSocket)
}

func TestDetectWithEnv_CmuxSocket_BadSocket(t *testing.T) {
	t.Setenv("CMUX_WORKSPACE_ID", "ws-123")
	t.Setenv("CMUX_SOCKET_PATH", "/nonexistent/cmux.sock")
	caps := DetectWithEnv("ghostty", nil)
	assert.False(t, caps.CmuxSocket)
}
