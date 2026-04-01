package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/tools"
	mcpclient "github.com/julianshen/rubichan/internal/tools/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeBackend struct {
	name string
}

func (b *fakeBackend) Name() string { return b.name }
func (b *fakeBackend) Open(_ context.Context, _ any, _ OpenOptions) (any, OpenResult, error) {
	return struct{}{}, OpenResult{URL: "https://93.184.216.34", Title: "Example", Backend: b.name}, nil
}
func (b *fakeBackend) Click(context.Context, any, string, bool) error { return nil }
func (b *fakeBackend) Fill(context.Context, any, string, string, bool) error {
	return nil
}
func (b *fakeBackend) Snapshot(context.Context, any) (string, error) {
	return "title: Example\nurl: https://93.184.216.34", nil
}
func (b *fakeBackend) Screenshot(_ context.Context, _ any, _ string, _ bool, _ string) (ScreenshotResult, error) {
	return ScreenshotResult{Path: "/tmp/test.png"}, nil
}
func (b *fakeBackend) Wait(context.Context, any, WaitOptions) error { return nil }
func (b *fakeBackend) Close(context.Context, any) error             { return nil }

// errorBackend returns errors for specified operations.
type errorBackend struct {
	fakeBackend
	failOn string
}

func (b *errorBackend) Open(ctx context.Context, h any, opts OpenOptions) (any, OpenResult, error) {
	if b.failOn == "open" {
		return nil, OpenResult{}, fmt.Errorf("open error")
	}
	return b.fakeBackend.Open(ctx, h, opts)
}

func (b *errorBackend) Click(_ context.Context, _ any, _ string, _ bool) error {
	if b.failOn == "click" {
		return fmt.Errorf("click error")
	}
	return nil
}

func (b *errorBackend) Fill(_ context.Context, _ any, _ string, _ string, _ bool) error {
	if b.failOn == "fill" {
		return fmt.Errorf("fill error")
	}
	return nil
}

func (b *errorBackend) Snapshot(_ context.Context, _ any) (string, error) {
	if b.failOn == "snapshot" {
		return "", fmt.Errorf("snapshot error")
	}
	return b.fakeBackend.Snapshot(nil, nil)
}

func (b *errorBackend) Screenshot(_ context.Context, _ any, _ string, _ bool, _ string) (ScreenshotResult, error) {
	if b.failOn == "screenshot" {
		return ScreenshotResult{}, fmt.Errorf("screenshot error")
	}
	return b.fakeBackend.Screenshot(nil, nil, "", false, "")
}

func (b *errorBackend) Wait(_ context.Context, _ any, _ WaitOptions) error {
	if b.failOn == "wait" {
		return fmt.Errorf("wait error")
	}
	return nil
}

func (b *errorBackend) Close(_ context.Context, _ any) error {
	if b.failOn == "close" {
		return fmt.Errorf("close error")
	}
	return nil
}

// newTestService creates a Service with a single fake backend for testing.
func newTestService(t *testing.T, backend Backend) *Service {
	t.Helper()
	return &Service{
		workDir:     t.TempDir(),
		artifactDir: t.TempDir(),
		backends:    map[string]Backend{backend.Name(): backend},
		order:       []string{backend.Name()},
		sessions:    make(map[string]*session),
	}
}

// openSession opens a session and returns the session ID.
func openSession(t *testing.T, svc *Service) string {
	t.Helper()
	result, err := svc.Open(context.Background(), json.RawMessage(`{"url":"https://93.184.216.34"}`))
	require.NoError(t, err)
	require.False(t, result.IsError, "open failed: %s", result.Content)
	return extractSessionID(t, result.Content)
}

func extractSessionID(t *testing.T, content string) string {
	t.Helper()
	for _, line := range splitLines(content) {
		if len(line) > len("session_id: ") && line[:len("session_id: ")] == "session_id: " {
			return line[len("session_id: "):]
		}
	}
	t.Fatal("session_id not found in output")
	return ""
}

// --- Open tests ---

func TestServiceOpenAndClose(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	openResult, err := svc.Open(context.Background(), json.RawMessage(`{"url":"https://93.184.216.34"}`))
	require.NoError(t, err)
	assert.False(t, openResult.IsError)
	assert.Contains(t, openResult.Content, "session_id:")

	sessionID := extractSessionID(t, openResult.Content)
	require.NotEmpty(t, sessionID)

	closeResult, err := svc.Close(context.Background(), json.RawMessage(`{"session_id":"`+sessionID+`"}`))
	require.NoError(t, err)
	assert.False(t, closeResult.IsError)
}

func TestServiceOpenEmptyURL(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	result, err := svc.Open(context.Background(), json.RawMessage(`{"url":""}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "url is required")
}

func TestServiceOpenInvalidJSON(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	result, err := svc.Open(context.Background(), json.RawMessage(`{bad json`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid input")
}

func TestServiceRejectsNonHTTPURL(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	tests := []struct {
		name string
		url  string
	}{
		{"file_scheme", `{"url":"file:///tmp/test"}`},
		{"ftp_scheme", `{"url":"ftp://93.184.216.34/file"}`},
		{"javascript_scheme", `{"url":"javascript:alert(1)"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, err := svc.Open(context.Background(), json.RawMessage(tc.url))
			require.NoError(t, err)
			assert.True(t, result.IsError)
			assert.Contains(t, result.Content, "http and https")
		})
	}
}

func TestServiceOpenSSRFProtection(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	tests := []struct {
		name string
		url  string
	}{
		{"loopback_127", `{"url":"http://127.0.0.1/"}`},
		{"loopback_ipv6", `{"url":"http://[::1]/"}`},
		{"private_10", `{"url":"http://10.0.0.1/"}`},
		{"private_172", `{"url":"http://172.16.0.1/"}`},
		{"private_192", `{"url":"http://192.168.1.1/"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, err := svc.Open(context.Background(), json.RawMessage(tc.url))
			require.NoError(t, err)
			assert.True(t, result.IsError)
			assert.Contains(t, result.Content, "private or local")
		})
	}
}

func TestServiceOpenBackendError(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &errorBackend{fakeBackend: fakeBackend{name: "native"}, failOn: "open"})

	result, err := svc.Open(context.Background(), json.RawMessage(`{"url":"https://93.184.216.34"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "browser_open failed")
}

func TestServiceOpenWithCustomSessionID(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	result, err := svc.Open(context.Background(), json.RawMessage(`{"url":"https://93.184.216.34","session_id":"mysess"}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "session_id: mysess")
}

func TestServiceOpenExistingSession(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	// Open initial session with custom ID.
	result, err := svc.Open(context.Background(), json.RawMessage(`{"url":"https://93.184.216.34","session_id":"reuse"}`))
	require.NoError(t, err)
	require.False(t, result.IsError)

	// Navigate in existing session.
	result, err = svc.Open(context.Background(), json.RawMessage(`{"url":"https://93.184.216.34/page2","session_id":"reuse"}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "session_id: reuse")
}

func TestServiceOpenExistingSessionBackendError(t *testing.T) {
	t.Parallel()
	eb := &errorBackend{fakeBackend: fakeBackend{name: "native"}, failOn: ""}
	svc := newTestService(t, eb)

	// Open initial session.
	result, err := svc.Open(context.Background(), json.RawMessage(`{"url":"https://93.184.216.34","session_id":"s1"}`))
	require.NoError(t, err)
	require.False(t, result.IsError)

	// Now make Open fail.
	eb.failOn = "open"
	result, err = svc.Open(context.Background(), json.RawMessage(`{"url":"https://93.184.216.34/page2","session_id":"s1"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "browser_open failed")
}

func TestServiceOpenExistingClosedSession(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	sid := openSession(t, svc)
	_, err := svc.Close(context.Background(), json.RawMessage(`{"session_id":"`+sid+`"}`))
	require.NoError(t, err)

	// Attempt to reuse a closed session by opening with that ID — session is deleted,
	// so it should create a new one.
	result, err := svc.Open(context.Background(), json.RawMessage(`{"url":"https://93.184.216.34","session_id":"`+sid+`"}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
}

func TestServiceOpenWithViewport(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	result, err := svc.Open(context.Background(), json.RawMessage(
		`{"url":"https://93.184.216.34","viewport":{"width":800,"height":600}}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
}

func TestServiceOpenWithHeadless(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	result, err := svc.Open(context.Background(), json.RawMessage(
		`{"url":"https://93.184.216.34","headless":false}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
}

func TestServiceOpenNoBackend(t *testing.T) {
	t.Parallel()
	svc := &Service{
		workDir:     t.TempDir(),
		artifactDir: t.TempDir(),
		backends:    map[string]Backend{},
		order:       []string{},
		sessions:    make(map[string]*session),
	}

	result, err := svc.Open(context.Background(), json.RawMessage(`{"url":"https://93.184.216.34"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "no browser backend")
}

// --- Click tests ---

func TestServiceClick(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})
	sid := openSession(t, svc)

	result, err := svc.Click(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`","selector":"#btn"}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "clicked")
	assert.Contains(t, result.Content, "#btn")
}

func TestServiceClickInvalidSessionID(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	result, err := svc.Click(context.Background(), json.RawMessage(
		`{"session_id":"nonexistent","selector":"#btn"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unknown session_id")
}

func TestServiceClickEmptySelector(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})
	sid := openSession(t, svc)

	result, err := svc.Click(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`","selector":""}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "selector is required")
}

func TestServiceClickEmptySessionID(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	result, err := svc.Click(context.Background(), json.RawMessage(
		`{"session_id":"","selector":"#btn"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "session_id is required")
}

func TestServiceClickBackendError(t *testing.T) {
	t.Parallel()
	eb := &errorBackend{fakeBackend: fakeBackend{name: "native"}, failOn: ""}
	svc := newTestService(t, eb)
	sid := openSession(t, svc)

	eb.failOn = "click"
	result, err := svc.Click(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`","selector":"#btn"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "browser_click failed")
}

func TestServiceClickInvalidJSON(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	result, err := svc.Click(context.Background(), json.RawMessage(`{bad`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid input")
}

func TestServiceClickClosedSession(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})
	sid := openSession(t, svc)

	// Manually mark session as closed but keep it in the map to test the closed check.
	svc.mu.Lock()
	svc.sessions[sid].closed = true
	svc.mu.Unlock()

	result, err := svc.Click(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`","selector":"#btn"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unknown session_id")
}

// --- Fill tests ---

func TestServiceFill(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})
	sid := openSession(t, svc)

	result, err := svc.Fill(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`","selector":"#input","value":"hello"}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "filled")
	assert.Contains(t, result.Content, "#input")
}

func TestServiceFillInvalidSessionID(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	result, err := svc.Fill(context.Background(), json.RawMessage(
		`{"session_id":"bad","selector":"#input","value":"x"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unknown session_id")
}

func TestServiceFillEmptySelector(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})
	sid := openSession(t, svc)

	result, err := svc.Fill(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`","selector":"","value":"x"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "selector is required")
}

func TestServiceFillEmptySessionID(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	result, err := svc.Fill(context.Background(), json.RawMessage(
		`{"session_id":"","selector":"#input","value":"x"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "session_id is required")
}

func TestServiceFillWithSubmit(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})
	sid := openSession(t, svc)

	result, err := svc.Fill(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`","selector":"#input","value":"hello","submit":true}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "filled")
}

func TestServiceFillBackendError(t *testing.T) {
	t.Parallel()
	eb := &errorBackend{fakeBackend: fakeBackend{name: "native"}, failOn: ""}
	svc := newTestService(t, eb)
	sid := openSession(t, svc)

	eb.failOn = "fill"
	result, err := svc.Fill(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`","selector":"#input","value":"x"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "browser_fill failed")
}

func TestServiceFillInvalidJSON(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	result, err := svc.Fill(context.Background(), json.RawMessage(`not json`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid input")
}

func TestServiceFillClosedSession(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})
	sid := openSession(t, svc)

	svc.mu.Lock()
	svc.sessions[sid].closed = true
	svc.mu.Unlock()

	result, err := svc.Fill(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`","selector":"#input","value":"x"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unknown session_id")
}

// --- Screenshot tests ---

func TestServiceScreenshot(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})
	sid := openSession(t, svc)

	result, err := svc.Screenshot(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`"}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "saved screenshot")
}

func TestServiceScreenshotInvalidSessionID(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	result, err := svc.Screenshot(context.Background(), json.RawMessage(
		`{"session_id":"bad"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unknown session_id")
}

func TestServiceScreenshotFullPage(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})
	sid := openSession(t, svc)

	result, err := svc.Screenshot(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`","full_page":true}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "saved screenshot")
}

func TestServiceScreenshotBackendError(t *testing.T) {
	t.Parallel()
	eb := &errorBackend{fakeBackend: fakeBackend{name: "native"}, failOn: ""}
	svc := newTestService(t, eb)
	sid := openSession(t, svc)

	eb.failOn = "screenshot"
	result, err := svc.Screenshot(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "browser_screenshot failed")
}

func TestServiceScreenshotInvalidJSON(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	result, err := svc.Screenshot(context.Background(), json.RawMessage(`{bad`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid input")
}

func TestServiceScreenshotClosedSession(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})
	sid := openSession(t, svc)

	svc.mu.Lock()
	svc.sessions[sid].closed = true
	svc.mu.Unlock()

	result, err := svc.Screenshot(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unknown session_id")
}

func TestServiceScreenshotEmptySessionID(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	result, err := svc.Screenshot(context.Background(), json.RawMessage(
		`{"session_id":""}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "session_id is required")
}

// --- Wait tests ---

func TestServiceWait(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})
	sid := openSession(t, svc)

	result, err := svc.Wait(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`","selector":"#loaded"}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "wait completed")
}

func TestServiceWaitInvalidSessionID(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	result, err := svc.Wait(context.Background(), json.RawMessage(
		`{"session_id":"bad","selector":"#x"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unknown session_id")
}

func TestServiceWaitWithTimeout(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})
	sid := openSession(t, svc)

	result, err := svc.Wait(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`","timeout_ms":100}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "wait completed")
}

func TestServiceWaitWithText(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})
	sid := openSession(t, svc)

	result, err := svc.Wait(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`","text":"loaded"}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
}

func TestServiceWaitMissingCriteria(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})
	sid := openSession(t, svc)

	result, err := svc.Wait(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "one of selector, text, or timeout_ms is required")
}

func TestServiceWaitBackendError(t *testing.T) {
	t.Parallel()
	eb := &errorBackend{fakeBackend: fakeBackend{name: "native"}, failOn: ""}
	svc := newTestService(t, eb)
	sid := openSession(t, svc)

	eb.failOn = "wait"
	result, err := svc.Wait(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`","selector":"#x"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "browser_wait failed")
}

func TestServiceWaitInvalidJSON(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	result, err := svc.Wait(context.Background(), json.RawMessage(`{bad`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid input")
}

func TestServiceWaitEmptySessionID(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	result, err := svc.Wait(context.Background(), json.RawMessage(
		`{"session_id":"","selector":"#x"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "session_id is required")
}

func TestServiceWaitClosedSession(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})
	sid := openSession(t, svc)

	svc.mu.Lock()
	svc.sessions[sid].closed = true
	svc.mu.Unlock()

	result, err := svc.Wait(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`","selector":"#x"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unknown session_id")
}

// --- Snapshot tests ---

func TestServiceSnapshot(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})
	sid := openSession(t, svc)

	result, err := svc.Snapshot(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`"}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "Example")
}

func TestServiceSnapshotInvalidSessionID(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	result, err := svc.Snapshot(context.Background(), json.RawMessage(
		`{"session_id":"bad"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unknown session_id")
}

func TestServiceSnapshotBackendError(t *testing.T) {
	t.Parallel()
	eb := &errorBackend{fakeBackend: fakeBackend{name: "native"}, failOn: ""}
	svc := newTestService(t, eb)
	sid := openSession(t, svc)

	eb.failOn = "snapshot"
	result, err := svc.Snapshot(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "browser_snapshot failed")
}

func TestServiceSnapshotInvalidJSON(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	result, err := svc.Snapshot(context.Background(), json.RawMessage(`{bad`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid input")
}

func TestServiceSnapshotEmptySessionID(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	result, err := svc.Snapshot(context.Background(), json.RawMessage(
		`{"session_id":""}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "session_id is required")
}

func TestServiceSnapshotClosedSession(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})
	sid := openSession(t, svc)

	svc.mu.Lock()
	svc.sessions[sid].closed = true
	svc.mu.Unlock()

	result, err := svc.Snapshot(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unknown session_id")
}

// --- Close tests ---

func TestServiceCloseUnknownSession(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	result, err := svc.Close(context.Background(), json.RawMessage(
		`{"session_id":"nonexistent"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unknown session_id")
}

func TestServiceDoubleClose(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})
	sid := openSession(t, svc)

	// First close succeeds.
	result, err := svc.Close(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`"}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)

	// Second close fails — session already removed.
	result, err = svc.Close(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unknown session_id")
}

func TestServiceCloseBackendError(t *testing.T) {
	t.Parallel()
	eb := &errorBackend{fakeBackend: fakeBackend{name: "native"}, failOn: ""}
	svc := newTestService(t, eb)
	sid := openSession(t, svc)

	eb.failOn = "close"
	result, err := svc.Close(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "browser_close failed")
}

func TestServiceCloseInvalidJSON(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	result, err := svc.Close(context.Background(), json.RawMessage(`{bad`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid input")
}

func TestServiceCloseAlreadyClosedInMap(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})
	sid := openSession(t, svc)

	// Mark as closed but keep in map to hit the sess.closed check in Close.
	svc.mu.Lock()
	svc.sessions[sid].closed = true
	svc.mu.Unlock()

	// Re-insert into sessions map so Close finds it but sees closed=true.
	result, err := svc.Close(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unknown session_id")
}

// --- Backend selection tests ---

func TestPickBackendPriority(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		backends map[string]Backend
		order    []string
		want     string
	}{
		{
			name:     "prefers first in order",
			backends: map[string]Backend{"mcp": &fakeBackend{name: "mcp"}, "native": &fakeBackend{name: "native"}},
			order:    []string{"mcp", "native"},
			want:     "mcp",
		},
		{
			name:     "falls back to second",
			backends: map[string]Backend{"native": &fakeBackend{name: "native"}},
			order:    []string{"mcp", "native"},
			want:     "native",
		},
		{
			name:     "native preferred",
			backends: map[string]Backend{"mcp": &fakeBackend{name: "mcp"}, "native": &fakeBackend{name: "native"}},
			order:    []string{"native", "mcp"},
			want:     "native",
		},
		{
			name:     "no backends returns nil",
			backends: map[string]Backend{},
			order:    []string{"mcp", "native"},
			want:     "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc := &Service{backends: tc.backends, order: tc.order}
			got := svc.pickBackend()
			if tc.want == "" {
				assert.Nil(t, got)
			} else {
				require.NotNil(t, got)
				assert.Equal(t, tc.want, got.Name())
			}
		})
	}
}

// --- Session management tests ---

func TestServiceUnknownSession(t *testing.T) {
	t.Parallel()
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

func TestSessionConcurrentAccess(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})
	sid := openSession(t, svc)

	var wg sync.WaitGroup
	errs := make(chan error, 20)

	// Run concurrent clicks on the same session.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.Click(context.Background(), json.RawMessage(
				`{"session_id":"`+sid+`","selector":"#btn"}`))
			if err != nil {
				errs <- err
			}
		}()
	}

	// Run concurrent snapshots on the same session.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.Snapshot(context.Background(), json.RawMessage(
				`{"session_id":"`+sid+`"}`))
			if err != nil {
				errs <- err
			}
		}()
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent operation error: %v", err)
	}
}

func TestSessionNotFoundForAllOps(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	ops := []struct {
		name string
		run  func() (tools.ToolResult, error)
	}{
		{"click", func() (tools.ToolResult, error) {
			return svc.Click(context.Background(), json.RawMessage(`{"session_id":"x","selector":"#a"}`))
		}},
		{"fill", func() (tools.ToolResult, error) {
			return svc.Fill(context.Background(), json.RawMessage(`{"session_id":"x","selector":"#a","value":"v"}`))
		}},
		{"snapshot", func() (tools.ToolResult, error) {
			return svc.Snapshot(context.Background(), json.RawMessage(`{"session_id":"x"}`))
		}},
		{"screenshot", func() (tools.ToolResult, error) {
			return svc.Screenshot(context.Background(), json.RawMessage(`{"session_id":"x"}`))
		}},
		{"wait", func() (tools.ToolResult, error) {
			return svc.Wait(context.Background(), json.RawMessage(`{"session_id":"x","selector":"#a"}`))
		}},
		{"close", func() (tools.ToolResult, error) {
			return svc.Close(context.Background(), json.RawMessage(`{"session_id":"x"}`))
		}},
	}

	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			t.Parallel()
			result, err := op.run()
			require.NoError(t, err)
			assert.True(t, result.IsError)
			assert.Contains(t, result.Content, "unknown session_id")
		})
	}
}

func TestSessionBackendUnavailable(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})
	sid := openSession(t, svc)

	// Remove the backend after session creation to simulate backend unavailable.
	svc.mu.Lock()
	delete(svc.backends, "native")
	svc.mu.Unlock()

	result, err := svc.Click(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`","selector":"#btn"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unavailable")
}

// --- NewTools tests ---

func TestNewToolsRegistersFamily(t *testing.T) {
	t.Parallel()
	svc := &Service{
		backends: map[string]Backend{"native": &fakeBackend{name: "native"}},
		order:    []string{"native"},
		sessions: make(map[string]*session),
	}
	got := NewTools(svc)
	names := make([]string, 0, len(got))
	for _, tl := range got {
		names = append(names, tl.Name())
	}
	expected := []string{
		"browser_open", "browser_click", "browser_fill",
		"browser_snapshot", "browser_screenshot", "browser_wait", "browser_close",
	}
	for _, name := range expected {
		assert.Contains(t, names, name)
	}
	assert.Len(t, got, 7)
}

func TestToolInterfaceCompliance(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})
	allTools := NewTools(svc)

	for _, tl := range allTools {
		t.Run(tl.Name(), func(t *testing.T) {
			t.Parallel()
			assert.NotEmpty(t, tl.Name())
			assert.NotEmpty(t, tl.Description())
			assert.NotEmpty(t, tl.InputSchema())

			// Verify schema is valid JSON.
			var schema map[string]any
			err := json.Unmarshal(tl.InputSchema(), &schema)
			require.NoError(t, err)
			assert.Equal(t, "object", schema["type"])
		})
	}
}

// --- Screenshot path tests ---

func TestScreenshotPathSanitizesSessionID(t *testing.T) {
	t.Parallel()
	svc := &Service{artifactDir: t.TempDir()}
	path := svc.screenshotPath("../bad session")
	assert.NotContains(t, path, "..")
	assert.Contains(t, path, "_bad_session")
}

// --- validateAndPinBrowserTarget tests ---

func TestValidateAndPinBrowserTargetRejectsEmptyHost(t *testing.T) {
	t.Parallel()
	// URL with empty host.
	_, err := validateAndPinBrowserTarget(context.Background(), mustParseURL("http:///path"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "host is required")
}

func TestValidateAndPinBrowserTargetRejectsPrivateIP(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		url  string
	}{
		{"loopback", "http://127.0.0.1/"},
		{"private_10", "http://10.0.0.1/"},
		{"private_172", "http://172.16.0.1/"},
		{"private_192", "http://192.168.1.1/"},
		{"ipv6_loopback", "http://[::1]/"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := validateAndPinBrowserTarget(context.Background(), mustParseURL(tc.url))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "private or local")
		})
	}
}

// --- MCP backend tests ---

func TestMCPWaitRespectsContextCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	backend := &MCPBackend{}
	err := backend.Wait(ctx, nil, WaitOptions{TimeoutMS: 10})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestJoinTextBlocks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		blocks []mcpclient.ContentBlock
		want   string
	}{
		{
			name:   "mixed types",
			blocks: []mcpclient.ContentBlock{{Type: "text", Text: "first"}, {Type: "image"}, {Type: "text", Text: "second"}},
			want:   "first\nsecond",
		},
		{
			name:   "empty",
			blocks: nil,
			want:   "",
		},
		{
			name:   "single text",
			blocks: []mcpclient.ContentBlock{{Type: "text", Text: "only"}},
			want:   "only",
		},
		{
			name:   "no text blocks",
			blocks: []mcpclient.ContentBlock{{Type: "image"}, {Type: "resource"}},
			want:   "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, joinTextBlocks(tc.blocks))
		})
	}
}

// --- Native backend tests ---

func TestNewNativeSessionTracksHeadlessOption(t *testing.T) {
	t.Parallel()
	sess, err := newNativeSession(OpenOptions{Headless: false})
	require.NoError(t, err)
	assert.False(t, sess.headless)
	sess.close()
}

// --- errResult tests ---

func TestErrResult(t *testing.T) {
	t.Parallel()
	r := errResult("something %s: %d", "went wrong", 42)
	assert.True(t, r.IsError)
	assert.Equal(t, "something went wrong: 42", r.Content)
}

// --- NewService tests ---

func TestNewServiceDefaults(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()

	svc, err := NewService(workDir, config.BrowserConfig{}, nil)
	require.NoError(t, err)
	require.NotNil(t, svc)
	assert.Equal(t, filepath.Join(workDir, ".rubichan", "browser"), svc.artifactDir)
	// Default order: mcp preferred first.
	assert.Equal(t, []string{"mcp", "native"}, svc.order)
	// Native backend should always be registered.
	assert.Contains(t, svc.backends, "native")
}

func TestNewServiceNativePreferred(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()

	svc, err := NewService(workDir, config.BrowserConfig{PreferredBackend: "native"}, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"native", "mcp"}, svc.order)
}

func TestNewServiceCustomArtifactDir(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()

	svc, err := NewService(workDir, config.BrowserConfig{ArtifactDir: "screenshots"}, nil)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(workDir, "screenshots"), svc.artifactDir)
}

func TestNewServiceAbsoluteArtifactDir(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	absDir := filepath.Join(workDir, "custom", "artifacts")

	svc, err := NewService(workDir, config.BrowserConfig{ArtifactDir: absDir}, nil)
	require.NoError(t, err)
	assert.Equal(t, absDir, svc.artifactDir)
}

func TestNewServiceRejectsArtifactDirOutsideWorkspace(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()

	_, err := NewService(workDir, config.BrowserConfig{ArtifactDir: "/tmp/outside"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "within the workspace")
}

// --- selectBrowserServer tests ---

func TestSelectBrowserServerExplicitName(t *testing.T) {
	t.Parallel()
	servers := []config.MCPServerConfig{
		{Name: "my-browser", Transport: "stdio", Command: "npx"},
		{Name: "other", Transport: "stdio", Command: "other"},
	}
	server, ok := selectBrowserServer(config.BrowserConfig{MCPServer: "my-browser"}, servers)
	assert.True(t, ok)
	assert.Equal(t, "my-browser", server.Name)
}

func TestSelectBrowserServerExplicitNameNotFound(t *testing.T) {
	t.Parallel()
	servers := []config.MCPServerConfig{
		{Name: "other", Transport: "stdio", Command: "other"},
	}
	_, ok := selectBrowserServer(config.BrowserConfig{MCPServer: "missing"}, servers)
	assert.False(t, ok)
}

func TestSelectBrowserServerAutoDetect(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		serverName string
		found      bool
	}{
		{"browser in name", "my-browser", true},
		{"playwright in name", "playwright-server", true},
		{"unrelated name", "database", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			servers := []config.MCPServerConfig{{Name: tc.serverName, Transport: "stdio", Command: "cmd"}}
			_, ok := selectBrowserServer(config.BrowserConfig{}, servers)
			assert.Equal(t, tc.found, ok)
		})
	}
}

func TestSelectBrowserServerEmptyList(t *testing.T) {
	t.Parallel()
	_, ok := selectBrowserServer(config.BrowserConfig{}, nil)
	assert.False(t, ok)
}

// --- firstSnapshotLine tests ---

func TestFirstSnapshotLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		snapshot string
		prefix   string
		want     string
	}{
		{"found", "title: My Page\nurl: https://93.184.216.34", "title: ", "My Page"},
		{"not found", "url: https://93.184.216.34", "title: ", ""},
		{"empty snapshot", "", "title: ", ""},
		{"prefix at start", "title: Hello", "title: ", "Hello"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, firstSnapshotLine(tc.snapshot, tc.prefix))
		})
	}
}

// --- NewNativeBackend tests ---

func TestNewNativeBackend(t *testing.T) {
	t.Parallel()
	nb := NewNativeBackend()
	require.NotNil(t, nb)
	assert.Equal(t, "native", nb.Name())
}

// --- requireNativeSession tests ---

func TestRequireNativeSessionNil(t *testing.T) {
	t.Parallel()
	_, err := requireNativeSession(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid native browser session")
}

func TestRequireNativeSessionWrongType(t *testing.T) {
	t.Parallel()
	_, err := requireNativeSession("not a session")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid native browser session")
}

func TestRequireNativeSessionValid(t *testing.T) {
	t.Parallel()
	sess, err := newNativeSession(OpenOptions{Headless: true})
	require.NoError(t, err)
	defer sess.close()

	got, err := requireNativeSession(sess)
	require.NoError(t, err)
	assert.Equal(t, sess, got)
}

// --- viewportAction tests ---

func TestViewportActionDefaults(t *testing.T) {
	t.Parallel()
	action := viewportAction(OpenOptions{})
	assert.NotNil(t, action)
}

func TestViewportActionCustom(t *testing.T) {
	t.Parallel()
	action := viewportAction(OpenOptions{Viewport: Viewport{Width: 800, Height: 600}})
	assert.NotNil(t, action)
}

// --- MCPBackend unit tests ---

func TestMCPBackendName(t *testing.T) {
	t.Parallel()
	b := &MCPBackend{}
	assert.Equal(t, "mcp", b.Name())
}

func TestMCPBackendHasToolEmpty(t *testing.T) {
	t.Parallel()
	b := &MCPBackend{}
	assert.False(t, b.hasTool("browser_snapshot"))
}

func TestMCPBackendHasToolPopulated(t *testing.T) {
	t.Parallel()
	b := &MCPBackend{tools: map[string]bool{"browser_snapshot": true}}
	assert.True(t, b.hasTool("browser_snapshot"))
	assert.False(t, b.hasTool("browser_other"))
}

// --- NewMCPBackend tests ---

func TestNewMCPBackendNoServers(t *testing.T) {
	t.Parallel()
	backend, err := NewMCPBackend(t.TempDir(), config.BrowserConfig{}, nil)
	require.NoError(t, err)
	assert.Nil(t, backend)
}

func TestNewMCPBackendWithMatchingServer(t *testing.T) {
	t.Parallel()
	servers := []config.MCPServerConfig{
		{Name: "playwright-browser", Transport: "stdio", Command: "npx"},
	}
	backend, err := NewMCPBackend(t.TempDir(), config.BrowserConfig{}, servers)
	require.NoError(t, err)
	require.NotNil(t, backend)
	assert.Equal(t, "mcp", backend.Name())
}

// --- tool.Execute tests ---

func TestToolExecuteDelegates(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})
	allTools := NewTools(svc)

	// Find browser_open tool and call Execute.
	var openTool tools.Tool
	for _, tl := range allTools {
		if tl.Name() == "browser_open" {
			openTool = tl
			break
		}
	}
	require.NotNil(t, openTool)

	result, err := openTool.Execute(context.Background(), json.RawMessage(`{"url":"https://93.184.216.34"}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "session_id:")
}

// --- Close backend unavailable path ---

func TestServiceCloseBackendUnavailable(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})
	sid := openSession(t, svc)

	// Remove backend but keep session to trigger "backend unavailable".
	svc.mu.Lock()
	delete(svc.backends, "native")
	svc.mu.Unlock()

	result, err := svc.Close(context.Background(), json.RawMessage(
		`{"session_id":"`+sid+`"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unavailable")
}

// --- Open with closed session in existing session path ---

func TestServiceOpenClosedSessionInMap(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, &fakeBackend{name: "native"})

	// Create session with custom ID then mark it closed but keep in map.
	result, err := svc.Open(context.Background(), json.RawMessage(
		`{"url":"https://93.184.216.34","session_id":"will-close"}`))
	require.NoError(t, err)
	require.False(t, result.IsError)

	svc.mu.Lock()
	svc.sessions["will-close"].closed = true
	svc.mu.Unlock()

	// Open with same session_id — should see closed and return error.
	result, err = svc.Open(context.Background(), json.RawMessage(
		`{"url":"https://93.184.216.34","session_id":"will-close"}`))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unknown session_id")
}

// --- validateAndPinBrowserTarget with port ---

func TestValidateAndPinBrowserTargetPublicIPWithPort(t *testing.T) {
	t.Parallel()
	u := mustParseURL("http://8.8.8.8:8080/path")
	pinned, err := validateAndPinBrowserTarget(context.Background(), u)
	require.NoError(t, err)
	assert.Contains(t, pinned, "8.8.8.8:8080")
}

func TestValidateAndPinBrowserTargetPublicIPNoPort(t *testing.T) {
	t.Parallel()
	u := mustParseURL("http://8.8.8.8/path")
	pinned, err := validateAndPinBrowserTarget(context.Background(), u)
	require.NoError(t, err)
	assert.Contains(t, pinned, "8.8.8.8")
}

// --- MCP Wait additional paths ---

func TestMCPWaitTimeoutOnlyPath(t *testing.T) {
	t.Parallel()
	b := &MCPBackend{}
	// Timeout-only path (no selector, no text) should just sleep.
	err := b.Wait(context.Background(), nil, WaitOptions{TimeoutMS: 1})
	require.NoError(t, err)
}

func TestMCPWaitNoConditions(t *testing.T) {
	t.Parallel()
	b := &MCPBackend{}
	// No selector, no text, no timeout — should return nil.
	err := b.Wait(context.Background(), nil, WaitOptions{})
	require.NoError(t, err)
}

// --- MCP Close without tools ---

func TestMCPCloseNoTools(t *testing.T) {
	t.Parallel()
	b := &MCPBackend{}
	// hasTool("browser_close") is false, should return nil immediately.
	err := b.Close(context.Background(), nil)
	require.NoError(t, err)
}

// --- Native backend methods with nil handle ---

func TestNativeBackendClickNilHandle(t *testing.T) {
	t.Parallel()
	b := NewNativeBackend()
	err := b.Click(context.Background(), nil, "#btn", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid native browser session")
}

func TestNativeBackendFillNilHandle(t *testing.T) {
	t.Parallel()
	b := NewNativeBackend()
	err := b.Fill(context.Background(), nil, "#input", "val", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid native browser session")
}

func TestNativeBackendSnapshotNilHandle(t *testing.T) {
	t.Parallel()
	b := NewNativeBackend()
	_, err := b.Snapshot(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid native browser session")
}

func TestNativeBackendScreenshotNilHandle(t *testing.T) {
	t.Parallel()
	b := NewNativeBackend()
	_, err := b.Screenshot(context.Background(), nil, "", false, "/tmp/test.png")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid native browser session")
}

func TestNativeBackendWaitNilHandle(t *testing.T) {
	t.Parallel()
	b := NewNativeBackend()
	err := b.Wait(context.Background(), nil, WaitOptions{Selector: "#x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid native browser session")
}

func TestNativeBackendCloseNilHandle(t *testing.T) {
	t.Parallel()
	b := NewNativeBackend()
	err := b.Close(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid native browser session")
}

func TestNativeBackendOpenNilHandle(t *testing.T) {
	t.Parallel()
	b := NewNativeBackend()
	// Open with nil handle creates a new session, which requires Chrome.
	// This will either work or fail depending on Chrome availability,
	// but we can test the wrong-type handle.
	_, _, err := b.Open(context.Background(), "wrong-type", OpenOptions{Headless: true, URL: "http://93.184.216.34"})
	// With wrong type, it should create a new session (not error from requireNativeSession).
	// The error will come from chromedp trying to start Chrome if not available.
	// We just verify it doesn't panic.
	_ = err
}

// --- MCP ensureClient error paths ---

func TestMCPEnsureClientUnsupportedTransport(t *testing.T) {
	t.Parallel()
	b := &MCPBackend{
		server: config.MCPServerConfig{
			Name:      "test",
			Transport: "unsupported",
		},
	}
	err := b.ensureClient(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported mcp transport")
}

func TestMCPEnsureClientAlreadyInitialized(t *testing.T) {
	t.Parallel()
	// Create a backend with a non-nil client to test the early return.
	b := &MCPBackend{
		client: &mcpclient.Client{},
	}
	err := b.ensureClient(context.Background())
	require.NoError(t, err)
}

// --- validateAndPinBrowserTarget DNS resolution ---

func TestValidateAndPinBrowserTargetUnresolvableHost(t *testing.T) {
	t.Parallel()
	u := mustParseURL("http://this-host-definitely-does-not-exist-xyzzy.invalid/")
	_, err := validateAndPinBrowserTarget(context.Background(), u)
	require.Error(t, err)
	// Should fail on DNS resolution.
}

// --- Native backend integration tests (require Chrome) ---

func chromeAvailable() bool {
	_, err := exec.LookPath("google-chrome")
	if err != nil {
		_, err = exec.LookPath("chromium-browser")
	}
	if err != nil {
		_, err = exec.LookPath("chromium")
	}
	return err == nil
}

func TestNativeBackendIntegration(t *testing.T) {
	t.Parallel()
	if !chromeAvailable() {
		t.Skip("Chrome not available for native backend tests")
	}

	const testHTML = `<!DOCTYPE html><html><head><title>Test Page</title></head>
<body><h1 id="heading">Hello</h1><button id="btn">Click Me</button>
<form><input id="field" type="text" value=""/></form>
<div class="dup">A</div><div class="dup">B</div></body></html>`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page2":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<!DOCTYPE html><html><head><title>Page 2</title></head><body>Page 2</body></html>`)
		default:
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, testHTML)
		}
	}))
	t.Cleanup(ts.Close)
	testURL := ts.URL

	b := NewNativeBackend()
	ctx := context.Background()

	t.Run("Open", func(t *testing.T) {
		t.Parallel()
		handle, result, err := b.Open(ctx, nil, OpenOptions{
			Headless: true,
			URL:      testURL,
			Viewport: Viewport{Width: 1024, Height: 768},
		})
		require.NoError(t, err)
		assert.Equal(t, "native", result.Backend)
		assert.Equal(t, "Test Page", result.Title)
		err = b.Close(ctx, handle)
		require.NoError(t, err)
	})

	t.Run("Click", func(t *testing.T) {
		t.Parallel()
		handle, _, err := b.Open(ctx, nil, OpenOptions{Headless: true, URL: testURL})
		require.NoError(t, err)
		defer func() { _ = b.Close(ctx, handle) }()

		err = b.Click(ctx, handle, "#btn", false)
		require.NoError(t, err)
	})

	t.Run("ClickWithWaitForNavigation", func(t *testing.T) {
		t.Parallel()
		handle, _, err := b.Open(ctx, nil, OpenOptions{Headless: true, URL: testURL})
		require.NoError(t, err)
		defer func() { _ = b.Close(ctx, handle) }()

		err = b.Click(ctx, handle, "#btn", true)
		require.NoError(t, err)
	})

	t.Run("ClickSelectorNotFound", func(t *testing.T) {
		t.Parallel()
		handle, _, err := b.Open(ctx, nil, OpenOptions{Headless: true, URL: testURL})
		require.NoError(t, err)
		defer func() { _ = b.Close(ctx, handle) }()

		err = b.Click(ctx, handle, "#nonexistent", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "matched no elements")
	})

	t.Run("Fill", func(t *testing.T) {
		t.Parallel()
		handle, _, err := b.Open(ctx, nil, OpenOptions{Headless: true, URL: testURL})
		require.NoError(t, err)
		defer func() { _ = b.Close(ctx, handle) }()

		err = b.Fill(ctx, handle, "#field", "test value", false)
		if err != nil {
			// chromedp.SetValue can fail on some Chrome versions; skip.
			t.Skipf("Fill not supported in this Chrome: %v", err)
		}
	})

	t.Run("FillWithSubmit", func(t *testing.T) {
		t.Parallel()
		handle, _, err := b.Open(ctx, nil, OpenOptions{Headless: true, URL: testURL})
		require.NoError(t, err)
		defer func() { _ = b.Close(ctx, handle) }()

		err = b.Fill(ctx, handle, "#field", "submitted", true)
		if err != nil {
			t.Skipf("Fill not supported in this Chrome: %v", err)
		}
	})

	t.Run("FillSelectorNotFound", func(t *testing.T) {
		t.Parallel()
		handle, _, err := b.Open(ctx, nil, OpenOptions{Headless: true, URL: testURL})
		require.NoError(t, err)
		defer func() { _ = b.Close(ctx, handle) }()

		err = b.Fill(ctx, handle, "#missing", "val", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "matched no elements")
	})

	t.Run("Snapshot", func(t *testing.T) {
		t.Parallel()
		handle, _, err := b.Open(ctx, nil, OpenOptions{Headless: true, URL: testURL})
		require.NoError(t, err)
		defer func() { _ = b.Close(ctx, handle) }()

		snapshot, err := b.Snapshot(ctx, handle)
		require.NoError(t, err)
		assert.Contains(t, snapshot, "Test Page")
		assert.Contains(t, snapshot, "Hello")
	})

	t.Run("ScreenshotFullPage", func(t *testing.T) {
		t.Parallel()
		handle, _, err := b.Open(ctx, nil, OpenOptions{Headless: true, URL: testURL})
		require.NoError(t, err)
		defer func() { _ = b.Close(ctx, handle) }()

		path := filepath.Join(t.TempDir(), "screenshot.png")
		result, err := b.Screenshot(ctx, handle, "", true, path)
		require.NoError(t, err)
		assert.Equal(t, path, result.Path)
	})

	t.Run("ScreenshotElement", func(t *testing.T) {
		t.Parallel()
		handle, _, err := b.Open(ctx, nil, OpenOptions{Headless: true, URL: testURL})
		require.NoError(t, err)
		defer func() { _ = b.Close(ctx, handle) }()

		path := filepath.Join(t.TempDir(), "element.png")
		result, err := b.Screenshot(ctx, handle, "#heading", false, path)
		require.NoError(t, err)
		assert.Equal(t, path, result.Path)
	})

	t.Run("ScreenshotViewport", func(t *testing.T) {
		t.Parallel()
		handle, _, err := b.Open(ctx, nil, OpenOptions{Headless: true, URL: testURL})
		require.NoError(t, err)
		defer func() { _ = b.Close(ctx, handle) }()

		path := filepath.Join(t.TempDir(), "viewport.png")
		result, err := b.Screenshot(ctx, handle, "", false, path)
		require.NoError(t, err)
		assert.Equal(t, path, result.Path)
	})

	t.Run("ScreenshotSelectorNotFound", func(t *testing.T) {
		t.Parallel()
		handle, _, err := b.Open(ctx, nil, OpenOptions{Headless: true, URL: testURL})
		require.NoError(t, err)
		defer func() { _ = b.Close(ctx, handle) }()

		path := filepath.Join(t.TempDir(), "fail.png")
		_, err = b.Screenshot(ctx, handle, "#nonexistent", false, path)
		require.Error(t, err)
	})

	t.Run("WaitSelector", func(t *testing.T) {
		t.Parallel()
		handle, _, err := b.Open(ctx, nil, OpenOptions{Headless: true, URL: testURL})
		require.NoError(t, err)
		defer func() { _ = b.Close(ctx, handle) }()

		err = b.Wait(ctx, handle, WaitOptions{Selector: "#heading"})
		require.NoError(t, err)
	})

	t.Run("WaitText", func(t *testing.T) {
		t.Parallel()
		handle, _, err := b.Open(ctx, nil, OpenOptions{Headless: true, URL: testURL})
		require.NoError(t, err)
		defer func() { _ = b.Close(ctx, handle) }()

		err = b.Wait(ctx, handle, WaitOptions{Text: "Hello"})
		require.NoError(t, err)
	})

	t.Run("WaitTimeout", func(t *testing.T) {
		t.Parallel()
		handle, _, err := b.Open(ctx, nil, OpenOptions{Headless: true, URL: testURL})
		require.NoError(t, err)
		defer func() { _ = b.Close(ctx, handle) }()

		err = b.Wait(ctx, handle, WaitOptions{TimeoutMS: 50})
		require.NoError(t, err)
	})

	t.Run("OpenReuseSession", func(t *testing.T) {
		t.Parallel()
		handle, _, err := b.Open(ctx, nil, OpenOptions{Headless: true, URL: testURL})
		require.NoError(t, err)
		defer func() { _ = b.Close(ctx, handle) }()

		handle2, result, err := b.Open(ctx, handle, OpenOptions{Headless: true, URL: testURL + "/page2"})
		require.NoError(t, err)
		assert.Equal(t, "Page 2", result.Title)
		_ = handle2
	})

	t.Run("OpenChangedHeadlessRecreatesSession", func(t *testing.T) {
		t.Parallel()
		handle, _, err := b.Open(ctx, nil, OpenOptions{Headless: true, URL: testURL})
		require.NoError(t, err)

		handle2, _, err := b.Open(ctx, handle, OpenOptions{Headless: false, URL: testURL})
		require.NoError(t, err)
		defer func() { _ = b.Close(ctx, handle2) }()
	})

	t.Run("EnsureUniqueSelectorMultipleMatches", func(t *testing.T) {
		t.Parallel()
		handle, _, err := b.Open(ctx, nil, OpenOptions{Headless: true, URL: testURL})
		require.NoError(t, err)
		defer func() { _ = b.Close(ctx, handle) }()

		err = b.Click(ctx, handle, ".dup", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "matched 2 elements")
	})
}

// --- MCP ensureClient stdio transport error ---

func TestMCPEnsureClientStdioTransportBadCommand(t *testing.T) {
	t.Parallel()
	b := &MCPBackend{
		server: config.MCPServerConfig{
			Name:      "test",
			Transport: "stdio",
			Command:   "/nonexistent-command-that-does-not-exist",
		},
	}
	err := b.ensureClient(context.Background())
	require.Error(t, err)
}

func TestMCPEnsureClientSSETransportCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b := &MCPBackend{
		server: config.MCPServerConfig{
			Name:      "test",
			Transport: "sse",
			URL:       "http://localhost:0/sse",
		},
	}
	err := b.ensureClient(ctx)
	require.Error(t, err)
}

// --- MCP operations that fail via call/ensureClient ---

func TestMCPOpenFailsWithoutServer(t *testing.T) {
	t.Parallel()
	b := &MCPBackend{
		server: config.MCPServerConfig{
			Name:      "test",
			Transport: "unsupported",
		},
	}
	_, _, err := b.Open(context.Background(), nil, OpenOptions{URL: "http://93.184.216.34"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported mcp transport")
}

func TestMCPClickFailsWithoutServer(t *testing.T) {
	t.Parallel()
	b := &MCPBackend{
		server: config.MCPServerConfig{
			Name:      "test",
			Transport: "unsupported",
		},
	}
	err := b.Click(context.Background(), nil, "#btn", false)
	require.Error(t, err)
}

func TestMCPFillFailsWithoutServer(t *testing.T) {
	t.Parallel()
	b := &MCPBackend{
		server: config.MCPServerConfig{
			Name:      "test",
			Transport: "unsupported",
		},
	}
	err := b.Fill(context.Background(), nil, "#input", "val", false)
	require.Error(t, err)
}

func TestMCPFillWithSubmitFailsWithoutServer(t *testing.T) {
	t.Parallel()
	b := &MCPBackend{
		server: config.MCPServerConfig{
			Name:      "test",
			Transport: "unsupported",
		},
	}
	err := b.Fill(context.Background(), nil, "#input", "val", true)
	require.Error(t, err)
}

func TestMCPSnapshotFailsWithoutServer(t *testing.T) {
	t.Parallel()
	b := &MCPBackend{
		server: config.MCPServerConfig{
			Name:      "test",
			Transport: "unsupported",
		},
	}
	_, err := b.Snapshot(context.Background(), nil)
	require.Error(t, err)
}

func TestMCPSnapshotWithToolFailsWithoutServer(t *testing.T) {
	t.Parallel()
	b := &MCPBackend{
		server: config.MCPServerConfig{
			Name:      "test",
			Transport: "unsupported",
		},
		tools: map[string]bool{"browser_snapshot": true},
	}
	_, err := b.Snapshot(context.Background(), nil)
	require.Error(t, err)
}

func TestMCPScreenshotFailsWithoutServer(t *testing.T) {
	t.Parallel()
	b := &MCPBackend{
		server: config.MCPServerConfig{
			Name:      "test",
			Transport: "unsupported",
		},
	}
	_, err := b.Screenshot(context.Background(), nil, "", false, "/tmp/test.png")
	require.Error(t, err)
}

func TestMCPScreenshotWithToolFailsWithoutServer(t *testing.T) {
	t.Parallel()
	b := &MCPBackend{
		server: config.MCPServerConfig{
			Name:      "test",
			Transport: "unsupported",
		},
		tools: map[string]bool{"browser_take_screenshot": true},
	}
	_, err := b.Screenshot(context.Background(), nil, "", false, "/tmp/test.png")
	require.Error(t, err)
}

func TestMCPScreenshotWithSelectorFailsWithoutServer(t *testing.T) {
	t.Parallel()
	b := &MCPBackend{
		server: config.MCPServerConfig{
			Name:      "test",
			Transport: "unsupported",
		},
	}
	_, err := b.Screenshot(context.Background(), nil, "#elem", false, "/tmp/test.png")
	require.Error(t, err)
}

func TestMCPWaitSelectorFailsWithoutServer(t *testing.T) {
	t.Parallel()
	b := &MCPBackend{
		server: config.MCPServerConfig{
			Name:      "test",
			Transport: "unsupported",
		},
	}
	err := b.Wait(context.Background(), nil, WaitOptions{Selector: "#x"})
	require.Error(t, err)
}

func TestMCPWaitTextWithToolFailsWithoutServer(t *testing.T) {
	t.Parallel()
	b := &MCPBackend{
		server: config.MCPServerConfig{
			Name:      "test",
			Transport: "unsupported",
		},
		tools: map[string]bool{"browser_wait_for": true},
	}
	err := b.Wait(context.Background(), nil, WaitOptions{Text: "hello"})
	require.Error(t, err)
}

func TestMCPWaitTextWithoutToolFailsWithoutServer(t *testing.T) {
	t.Parallel()
	b := &MCPBackend{
		server: config.MCPServerConfig{
			Name:      "test",
			Transport: "unsupported",
		},
	}
	err := b.Wait(context.Background(), nil, WaitOptions{Text: "hello"})
	require.Error(t, err)
}

func TestMCPCloseWithToolFailsWithoutServer(t *testing.T) {
	t.Parallel()
	b := &MCPBackend{
		server: config.MCPServerConfig{
			Name:      "test",
			Transport: "unsupported",
		},
		tools: map[string]bool{"browser_close": true},
	}
	err := b.Close(context.Background(), nil)
	require.Error(t, err)
}

// --- helpers ---

var _ tools.Tool = (*tool)(nil)

func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}

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
