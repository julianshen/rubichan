package team

import (
	"strings"
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/require"
)

type mockTmuxController struct {
	available       bool
	sessions        map[string]bool
	windows         map[string]string
	sentTexts       []string
	createSessionFn func(string) error
}

func (m *mockTmuxController) Available() bool { return m.available }
func (m *mockTmuxController) SessionExists(name string) bool {
	return m.sessions[name]
}
func (m *mockTmuxController) KillSession(name string) error {
	delete(m.sessions, name)
	return nil
}
func (m *mockTmuxController) CreateSession(name string) error {
	if m.createSessionFn != nil {
		return m.createSessionFn(name)
	}
	m.sessions[name] = true
	return nil
}
func (m *mockTmuxController) CreateWindow(sessionName, windowName string) (string, error) {
	paneID := sessionName + ":" + windowName
	m.windows[windowName] = paneID
	return paneID, nil
}
func (m *mockTmuxController) SendText(paneID, text string) error {
	m.sentTexts = append(m.sentTexts, text)
	return nil
}

func TestMockTmuxController(t *testing.T) {
	m := &mockTmuxController{
		available: true,
		sessions:  make(map[string]bool),
		windows:   make(map[string]string),
	}
	require.True(t, m.Available())
	require.NoError(t, m.CreateSession("test"))
	require.True(t, m.SessionExists("test"))
	paneID, err := m.CreateWindow("test", "agent1")
	require.NoError(t, err)
	require.Equal(t, "test:agent1", paneID)
}

func TestTmuxDisplayStart(t *testing.T) {
	m := &mockTmuxController{
		available: true,
		sessions:  make(map[string]bool),
		windows:   make(map[string]string),
	}
	d := NewTmuxDisplay(m)
	err := d.Start()
	require.NoError(t, err)
	require.True(t, d.IsActive())
	require.True(t, m.SessionExists("rubichan-swarm"))
}

func TestTmuxDisplayDisabledWhenTmuxUnavailable(t *testing.T) {
	m := &mockTmuxController{available: false}
	d := NewTmuxDisplay(m)
	err := d.Start()
	require.NoError(t, err)
	require.False(t, d.IsActive())
}

func TestTmuxDisplayAddAgent(t *testing.T) {
	m := &mockTmuxController{
		available: true,
		sessions:  make(map[string]bool),
		windows:   make(map[string]string),
	}
	d := NewTmuxDisplay(m)
	_ = d.Start()

	paneID, err := d.AddAgent("agent-1", "explorer", "blue")
	require.NoError(t, err)
	require.Equal(t, "rubichan-swarm:explorer", paneID)
	require.Len(t, m.sentTexts, 1)
	require.Contains(t, m.sentTexts[0], "Agent: explorer")
}

func TestTmuxDisplayWriteEvent(t *testing.T) {
	m := &mockTmuxController{
		available: true,
		sessions:  make(map[string]bool),
		windows:   make(map[string]string),
	}
	d := NewTmuxDisplay(m)
	_ = d.Start()
	_, _ = d.AddAgent("agent-1", "explorer", "blue")

	err := d.WriteEvent("agent-1", agentsdk.DisplayMessage{
		Role: "assistant",
		Content: []agentsdk.ContentBlock{
			{Type: agentsdk.BlockTypeText, Text: "hello world"},
		},
	})
	require.NoError(t, err)
	require.Len(t, m.sentTexts, 2) // header + message
	require.Contains(t, m.sentTexts[1], "hello world")
}

func TestTmuxDisplayMarkDone(t *testing.T) {
	m := &mockTmuxController{
		available: true,
		sessions:  make(map[string]bool),
		windows:   make(map[string]string),
	}
	d := NewTmuxDisplay(m)
	_ = d.Start()
	_, _ = d.AddAgent("agent-1", "explorer", "blue")

	err := d.MarkDone("agent-1")
	require.NoError(t, err)
	require.Len(t, m.sentTexts, 2)
	require.Contains(t, m.sentTexts[1], "finished")
}

func TestTmuxDisplayCleanup(t *testing.T) {
	m := &mockTmuxController{
		available: true,
		sessions:  make(map[string]bool),
		windows:   make(map[string]string),
	}
	d := NewTmuxDisplay(m)
	_ = d.Start()
	_, _ = d.AddAgent("agent-1", "explorer", "blue")

	err := d.Cleanup()
	require.NoError(t, err)
	require.False(t, d.IsActive())
	require.False(t, m.SessionExists("rubichan-swarm"))
}

func TestTmuxDisplayCleanupDisabled(t *testing.T) {
	m := &mockTmuxController{available: false}
	d := NewTmuxDisplay(m)
	_ = d.Start()

	err := d.Cleanup()
	require.NoError(t, err)
	require.False(t, d.IsActive())
}

func TestSanitizeWindowName(t *testing.T) {
	require.Equal(t, "foo-bar", sanitizeWindowName("foo:bar"))
	require.Equal(t, "foo-bar", sanitizeWindowName("foo.bar"))
	require.Equal(t, "a-b-c", sanitizeWindowName("a:b.c"))
}

func TestFormatMessage(t *testing.T) {
	msg := agentsdk.DisplayMessage{
		Role: "assistant",
		Content: []agentsdk.ContentBlock{
			{Type: agentsdk.BlockTypeText, Text: "hello"},
			{Type: agentsdk.BlockTypeToolUse, Name: "shell", Input: []byte(`{"command": "ls"}`)},
			{Type: agentsdk.BlockTypeToolResult, Text: "result content", IsError: false},
		},
	}
	text := formatMessage(msg)
	require.Contains(t, text, "hello")
	require.Contains(t, text, "[tool_use] shell(command)")
	require.Contains(t, text, "[tool_result] result content")
}

func TestFormatMessageEmpty(t *testing.T) {
	msg := agentsdk.DisplayMessage{Role: "assistant"}
	require.Empty(t, formatMessage(msg))
}

func TestFormatMessageToolResultError(t *testing.T) {
	msg := agentsdk.DisplayMessage{
		Role: "assistant",
		Content: []agentsdk.ContentBlock{
			{Type: agentsdk.BlockTypeToolResult, Text: "error output", IsError: true},
		},
	}
	text := formatMessage(msg)
	require.Contains(t, text, "[tool_result:error]")
	require.Contains(t, text, "error output")
}

func TestFormatMessageTruncate(t *testing.T) {
	longText := strings.Repeat("a", 250)
	msg := agentsdk.DisplayMessage{
		Role: "assistant",
		Content: []agentsdk.ContentBlock{
			{Type: agentsdk.BlockTypeToolResult, Text: longText, IsError: false},
		},
	}
	text := formatMessage(msg)
	require.Len(t, text, len("[15:04:05] [tool_result] ")+200+3)
	require.True(t, strings.HasSuffix(text, "..."))
}
