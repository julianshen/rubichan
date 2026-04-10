package terminal

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMockProberPipe creates a pipe-based mock terminal that writes a canned
// response when the prober sends a query containing trigger. Uses io.Pipe to
// avoid the race condition inherent in sharing a strings.Builder across goroutines.
func newMockProberPipe(t *testing.T, trigger, response string) (io.Reader, io.Writer) {
	t.Helper()
	pr, pw := io.Pipe()
	// Writer side: the prober writes queries here; we discard them.
	queryR, queryW := io.Pipe()
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := queryR.Read(buf)
			if err != nil {
				return
			}
			if strings.Contains(string(buf[:n]), trigger) {
				pw.Write([]byte(response))
			}
		}
	}()
	t.Cleanup(func() {
		queryW.Close()
		pw.Close()
	})
	return pr, queryW
}

// newSilentProberPipe creates a pipe that never responds (for timeout tests).
// The write side discards all queries so the prober's writes don't block.
func newSilentProberPipe(t *testing.T) (io.Reader, io.Writer) {
	t.Helper()
	pr, pw := io.Pipe()
	t.Cleanup(func() { pw.Close() })
	// Write side: a pipe whose reader drains in the background so writes don't block.
	queryR, queryW := io.Pipe()
	go func() {
		buf := make([]byte, 256)
		for {
			if _, err := queryR.Read(buf); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() { queryW.Close() })
	return pr, queryW
}

func TestStdioProber_ProbeBackground_Dark(t *testing.T) {
	r, w := newMockProberPipe(t, "\x1b]11;?", "\x1b]11;rgb:1a1a/1a1a/1a1a\x1b\\")
	prober := NewStdioProber(r, w, 500*time.Millisecond)
	isDark, supported := prober.ProbeBackground()
	require.True(t, supported)
	assert.True(t, isDark)
}

func TestStdioProber_ProbeBackground_Light(t *testing.T) {
	r, w := newMockProberPipe(t, "\x1b]11;?", "\x1b]11;rgb:f0f0/f0f0/f0f0\x1b\\")
	prober := NewStdioProber(r, w, 500*time.Millisecond)
	isDark, supported := prober.ProbeBackground()
	require.True(t, supported)
	assert.False(t, isDark)
}

func TestStdioProber_ProbeBackground_Timeout(t *testing.T) {
	r, w := newSilentProberPipe(t)
	prober := NewStdioProber(r, w, 10*time.Millisecond)
	_, supported := prober.ProbeBackground()
	assert.False(t, supported)
}

func TestStdioProber_ProbeSyncRendering_Supported(t *testing.T) {
	r, w := newMockProberPipe(t, "\x1b[?2026$p", "\x1b[?2026;1$y")
	prober := NewStdioProber(r, w, 500*time.Millisecond)
	assert.True(t, prober.ProbeSyncRendering())
}

func TestStdioProber_ProbeSyncRendering_Unsupported(t *testing.T) {
	r, w := newSilentProberPipe(t)
	prober := NewStdioProber(r, w, 10*time.Millisecond)
	assert.False(t, prober.ProbeSyncRendering())
}

func TestStdioProber_ProbeKittyKeyboard_Supported(t *testing.T) {
	r, w := newMockProberPipe(t, "\x1b[?u", "\x1b[?1u")
	prober := NewStdioProber(r, w, 500*time.Millisecond)
	assert.True(t, prober.ProbeKittyKeyboard())
}

func TestStdioProber_ProbeKittyKeyboard_Unsupported(t *testing.T) {
	r, w := newSilentProberPipe(t)
	prober := NewStdioProber(r, w, 10*time.Millisecond)
	assert.False(t, prober.ProbeKittyKeyboard())
}

func TestDetectWithEnv_UnknownTerminal_WithProber(t *testing.T) {
	r, w := newMockProberPipe(t, "\x1b]11;?", "\x1b]11;rgb:1a1a/1a1a/1a1a\x1b\\")
	prober := NewStdioProber(r, w, 500*time.Millisecond)
	caps := DetectWithEnv("unknown-terminal-xyz", prober)
	assert.True(t, caps.DarkBackground)
	assert.True(t, caps.LightDarkMode)
}

func TestDetectWithEnv_KnownTerminal_WithProber_LightBackground(t *testing.T) {
	r, w := newMockProberPipe(t, "\x1b]11;?", "\x1b]11;rgb:f0f0/f0f0/f0f0\x1b\\")
	prober := NewStdioProber(r, w, 500*time.Millisecond)
	caps := DetectWithEnv("ghostty", prober)
	// Prober overrides the default DarkBackground=true for known terminals.
	assert.False(t, caps.DarkBackground)
	// Other capabilities remain from the known-terminal table.
	assert.True(t, caps.Hyperlinks)
	assert.True(t, caps.KittyGraphics)
	assert.True(t, caps.ProgressBar)
}
