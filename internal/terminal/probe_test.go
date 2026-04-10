package terminal

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTerminal provides a fake terminal that responds to known escape sequences.
type mockTerminal struct {
	responses map[string]string
	buf       strings.Builder
}

func (m *mockTerminal) Write(p []byte) (n int, err error) {
	m.buf.Write(p)
	return len(p), nil
}

func (m *mockTerminal) Read(p []byte) (n int, err error) {
	query := m.buf.String()
	m.buf.Reset()
	for trigger, resp := range m.responses {
		if strings.Contains(query, trigger) {
			n = copy(p, resp)
			if n < len(resp) {
				return n, nil
			}
			return n, io.EOF
		}
	}
	return 0, io.EOF
}

func TestStdioProber_ProbeBackground_Dark(t *testing.T) {
	mt := &mockTerminal{
		responses: map[string]string{
			"\x1b]11;?": "\x1b]11;rgb:1a1a/1a1a/1a1a\x1b\\",
		},
	}
	prober := NewStdioProber(mt, mt, 500*time.Millisecond)
	isDark, supported := prober.ProbeBackground()
	require.True(t, supported)
	assert.True(t, isDark)
}

func TestStdioProber_ProbeBackground_Light(t *testing.T) {
	mt := &mockTerminal{
		responses: map[string]string{
			"\x1b]11;?": "\x1b]11;rgb:f0f0/f0f0/f0f0\x1b\\",
		},
	}
	prober := NewStdioProber(mt, mt, 500*time.Millisecond)
	isDark, supported := prober.ProbeBackground()
	require.True(t, supported)
	assert.False(t, isDark)
}

func TestStdioProber_ProbeBackground_Timeout(t *testing.T) {
	mt := &mockTerminal{
		responses: map[string]string{},
	}
	prober := NewStdioProber(mt, mt, 10*time.Millisecond)
	_, supported := prober.ProbeBackground()
	assert.False(t, supported)
}

func TestDetectWithEnv_UnknownTerminal_WithProber(t *testing.T) {
	mt := &mockTerminal{
		responses: map[string]string{
			"\x1b]11;?": "\x1b]11;rgb:1a1a/1a1a/1a1a\x1b\\",
		},
	}
	prober := NewStdioProber(mt, mt, 500*time.Millisecond)
	caps := DetectWithEnv("unknown-terminal-xyz", prober)
	assert.True(t, caps.DarkBackground)
	assert.True(t, caps.LightDarkMode)
}
