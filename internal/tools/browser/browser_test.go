package browser

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/tools"
	mcpclient "github.com/julianshen/rubichan/internal/tools/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeBackend struct {
	name string
}

func (b *fakeBackend) Name() string { return b.name }
func (b *fakeBackend) Open(context.Context, any, OpenOptions) (any, OpenResult, error) {
	return struct{}{}, OpenResult{URL: "https://example.com", Title: "Example", Backend: b.name}, nil
}
func (b *fakeBackend) Click(context.Context, any, string, bool) error { return nil }
func (b *fakeBackend) Fill(context.Context, any, string, string, bool) error {
	return nil
}
func (b *fakeBackend) Snapshot(context.Context, any) (string, error) {
	return "title: Example\nurl: https://example.com", nil
}
func (b *fakeBackend) Screenshot(context.Context, any, string, bool, string) (ScreenshotResult, error) {
	return ScreenshotResult{Path: "/tmp/test.png"}, nil
}
func (b *fakeBackend) Wait(context.Context, any, WaitOptions) error { return nil }
func (b *fakeBackend) Close(context.Context, any) error             { return nil }

func TestServiceOpenAndClose(t *testing.T) {
	svc := &Service{
		workDir:     t.TempDir(),
		artifactDir: t.TempDir(),
		backends:    map[string]Backend{"native": &fakeBackend{name: "native"}},
		order:       []string{"native"},
		sessions:    make(map[string]*session),
	}

	openResult, err := svc.Open(context.Background(), json.RawMessage(`{"url":"https://example.com"}`))
	require.NoError(t, err)
	assert.False(t, openResult.IsError)
	assert.Contains(t, openResult.Content, "session_id:")

	var sessionID string
	for _, line := range splitLines(openResult.Content) {
		if len(line) > len("session_id: ") && line[:len("session_id: ")] == "session_id: " {
			sessionID = line[len("session_id: "):]
		}
	}
	require.NotEmpty(t, sessionID)

	closeResult, err := svc.Close(context.Background(), json.RawMessage(`{"session_id":"`+sessionID+`"}`))
	require.NoError(t, err)
	assert.False(t, closeResult.IsError)
}

func TestServiceUnknownSession(t *testing.T) {
	svc := &Service{
		backends: map[string]Backend{"native": &fakeBackend{name: "native"}},
		order:    []string{"native"},
		sessions: make(map[string]*session),
	}
	result, err := svc.Click(context.Background(), json.RawMessage(`{"session_id":"missing","selector":"#go"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unknown session_id")
}

func TestNewToolsRegistersFamily(t *testing.T) {
	svc := &Service{
		backends: map[string]Backend{"native": &fakeBackend{name: "native"}},
		order:    []string{"native"},
		sessions: make(map[string]*session),
	}
	got := NewTools(svc)
	names := make([]string, 0, len(got))
	for _, tool := range got {
		names = append(names, tool.Name())
	}
	assert.Contains(t, names, "browser_open")
	assert.Contains(t, names, "browser_close")
}

func TestScreenshotPathSanitizesSessionID(t *testing.T) {
	svc := &Service{artifactDir: t.TempDir()}
	path := svc.screenshotPath("../bad session")
	assert.NotContains(t, path, "..")
	assert.Contains(t, path, "_bad_session")
}

func TestServiceRejectsNonHTTPURL(t *testing.T) {
	svc := &Service{
		backends: map[string]Backend{"native": &fakeBackend{name: "native"}},
		order:    []string{"native"},
		sessions: make(map[string]*session),
	}
	result, err := svc.Open(context.Background(), json.RawMessage(`{"url":"file:///tmp/test"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "http and https")
}

func TestMCPWaitRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	backend := &MCPBackend{}
	err := backend.Wait(ctx, nil, WaitOptions{TimeoutMS: 10})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestJoinTextBlocks(t *testing.T) {
	got := joinTextBlocks([]mcpclient.ContentBlock{
		{Type: "text", Text: "first"},
		{Type: "image"},
		{Type: "text", Text: "second"},
	})
	assert.Equal(t, "first\nsecond", got)
}

func TestNewNativeSessionTracksHeadlessOption(t *testing.T) {
	sess, err := newNativeSession(OpenOptions{Headless: false})
	require.NoError(t, err)
	assert.False(t, sess.headless)
	sess.close()
}

var _ tools.Tool = (*tool)(nil)

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start <= len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
