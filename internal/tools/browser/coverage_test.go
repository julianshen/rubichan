package browser

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/config"
	mcpclient "github.com/julianshen/rubichan/internal/tools/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockMCPTransport is a JSON-RPC transport that replays canned responses and
// lets tests inject a real mcpclient.Client into an MCPBackend.
type mockMCPTransport struct {
	sent      []json.RawMessage
	responses []json.RawMessage
	idx       int
}

func (m *mockMCPTransport) Send(_ context.Context, msg any) error {
	data, _ := json.Marshal(msg)
	m.sent = append(m.sent, data)
	return nil
}

func (m *mockMCPTransport) Receive(_ context.Context, result any) error {
	if m.idx >= len(m.responses) {
		return io.EOF
	}
	resp := m.responses[m.idx]
	m.idx++
	return json.Unmarshal(resp, result)
}

func (m *mockMCPTransport) Close() error { return nil }

// newMCPBackendWithClient constructs an MCPBackend with a pre-built client
// that uses a mock transport. The responses slice must start with the
// tool-call JSON-RPC response(s) the test expects.
func newMCPBackendWithClient(t *testing.T, responses []json.RawMessage, tools map[string]bool) *MCPBackend {
	t.Helper()
	mt := &mockMCPTransport{responses: responses}
	client := mcpclient.NewClient("mock", mt)
	return &MCPBackend{
		client: client,
		tools:  tools,
	}
}

// -------- MCPBackend.Open happy-path --------

// TestMCPOpen_Success exercises Open when navigate and snapshot both succeed.
func TestMCPOpen_Success(t *testing.T) {
	t.Parallel()
	responses := []json.RawMessage{
		// browser_navigate
		json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`),
		// Snapshot falls back to browser_run_code (no browser_snapshot tool).
		json.RawMessage(`{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"title: My Page\nurl: https://example.com"}]}}`),
	}
	b := newMCPBackendWithClient(t, responses, nil)

	_, result, err := b.Open(context.Background(), nil, OpenOptions{URL: "https://example.com"})
	require.NoError(t, err)
	assert.Equal(t, "https://example.com", result.URL)
	assert.Equal(t, "mcp", result.Backend)
}

// TestMCPOpen_SnapshotIgnored covers the Snapshot-error-ignored branch.
func TestMCPOpen_SnapshotIgnored(t *testing.T) {
	t.Parallel()
	responses := []json.RawMessage{
		// browser_navigate succeeds.
		json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`),
		// Snapshot via browser_run_code returns an error — Open swallows it.
		json.RawMessage(`{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"boom"}],"isError":true}}`),
	}
	b := newMCPBackendWithClient(t, responses, nil)

	_, result, err := b.Open(context.Background(), nil, OpenOptions{URL: "https://example.com"})
	require.NoError(t, err)
	assert.Equal(t, "mcp", result.Backend)
}

// TestMCPOpen_NavigateError exercises the navigate failure branch.
func TestMCPOpen_NavigateError(t *testing.T) {
	t.Parallel()
	responses := []json.RawMessage{
		json.RawMessage(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"navigate failed"}}`),
	}
	b := newMCPBackendWithClient(t, responses, nil)

	_, _, err := b.Open(context.Background(), nil, OpenOptions{URL: "https://example.com"})
	require.Error(t, err)
}

// -------- MCPBackend.Click happy-path + waitForNavigation --------

// TestMCPClick_Success covers the click-success branch.
func TestMCPClick_Success(t *testing.T) {
	t.Parallel()
	responses := []json.RawMessage{
		json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`),
	}
	b := newMCPBackendWithClient(t, responses, nil)
	err := b.Click(context.Background(), nil, "#btn", false)
	require.NoError(t, err)
}

// TestMCPClick_WithWaitForNavigation covers the waitForNavigation=true branch.
func TestMCPClick_WithWaitForNavigation(t *testing.T) {
	t.Parallel()
	responses := []json.RawMessage{
		// click (browser_run_code)
		json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`),
	}
	b := newMCPBackendWithClient(t, responses, nil)
	// Wait with TimeoutMS only goes down the sleep-branch (no RPC).
	err := b.Click(context.Background(), nil, "#btn", true)
	require.NoError(t, err)
}

// -------- MCPBackend.Fill happy-path + submit --------

// TestMCPFill_Success covers the basic fill branch.
func TestMCPFill_Success(t *testing.T) {
	t.Parallel()
	responses := []json.RawMessage{
		json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`),
	}
	b := newMCPBackendWithClient(t, responses, nil)
	err := b.Fill(context.Background(), nil, "#input", "hi", false)
	require.NoError(t, err)
}

// TestMCPFill_Submit covers the submit=true branch.
func TestMCPFill_Submit(t *testing.T) {
	t.Parallel()
	responses := []json.RawMessage{
		json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`),
	}
	b := newMCPBackendWithClient(t, responses, nil)
	err := b.Fill(context.Background(), nil, "#input", "hi", true)
	require.NoError(t, err)
}

// -------- MCPBackend.Snapshot happy-path --------

// TestMCPSnapshot_WithTool covers the browser_snapshot-tool branch.
func TestMCPSnapshot_WithTool(t *testing.T) {
	t.Parallel()
	responses := []json.RawMessage{
		json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"title: Page\nurl: /"}]}}`),
	}
	b := newMCPBackendWithClient(t, responses, map[string]bool{"browser_snapshot": true})
	got, err := b.Snapshot(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, got, "title: Page")
}

// TestMCPSnapshot_FallbackRunCode covers the fallback-via-browser_run_code branch.
func TestMCPSnapshot_FallbackRunCode(t *testing.T) {
	t.Parallel()
	responses := []json.RawMessage{
		json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"fallback-snapshot"}]}}`),
	}
	b := newMCPBackendWithClient(t, responses, nil)
	got, err := b.Snapshot(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, "fallback-snapshot", got)
}

// -------- MCPBackend.Screenshot happy-paths --------

// TestMCPScreenshot_TakeScreenshotTool covers the browser_take_screenshot branch.
func TestMCPScreenshot_TakeScreenshotTool(t *testing.T) {
	t.Parallel()
	responses := []json.RawMessage{
		json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"saved"}]}}`),
	}
	b := newMCPBackendWithClient(t, responses, map[string]bool{"browser_take_screenshot": true})
	path := filepath.Join(t.TempDir(), "s.png")
	res, err := b.Screenshot(context.Background(), nil, "", false, path)
	require.NoError(t, err)
	assert.Equal(t, path, res.Path)
}

// TestMCPScreenshot_RunCodeFullPage covers the browser_run_code no-selector branch.
func TestMCPScreenshot_RunCodeFullPage(t *testing.T) {
	t.Parallel()
	responses := []json.RawMessage{
		json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"saved"}]}}`),
	}
	b := newMCPBackendWithClient(t, responses, nil)
	path := filepath.Join(t.TempDir(), "full.png")
	res, err := b.Screenshot(context.Background(), nil, "", true, path)
	require.NoError(t, err)
	assert.Equal(t, path, res.Path)
}

// TestMCPScreenshot_RunCodeWithSelector covers the browser_run_code selector branch.
func TestMCPScreenshot_RunCodeWithSelector(t *testing.T) {
	t.Parallel()
	responses := []json.RawMessage{
		json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"saved"}]}}`),
	}
	b := newMCPBackendWithClient(t, responses, nil)
	path := filepath.Join(t.TempDir(), "sel.png")
	res, err := b.Screenshot(context.Background(), nil, "#elem", false, path)
	require.NoError(t, err)
	assert.Equal(t, path, res.Path)
}

// -------- MCPBackend.Wait paths via mock client --------

// TestMCPWait_WithToolWaitFor covers the browser_wait_for tool branch.
func TestMCPWait_WithToolWaitFor(t *testing.T) {
	t.Parallel()
	responses := []json.RawMessage{
		json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`),
	}
	b := newMCPBackendWithClient(t, responses, map[string]bool{"browser_wait_for": true})
	err := b.Wait(context.Background(), nil, WaitOptions{Text: "loaded"})
	require.NoError(t, err)
}

// TestMCPWait_RunCodeSelector covers the selector-via-run_code path.
func TestMCPWait_RunCodeSelector(t *testing.T) {
	t.Parallel()
	responses := []json.RawMessage{
		json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`),
	}
	b := newMCPBackendWithClient(t, responses, nil)
	err := b.Wait(context.Background(), nil, WaitOptions{Selector: "#x"})
	require.NoError(t, err)
}

// TestMCPWait_RunCodeText covers the text-via-run_code fallback path.
func TestMCPWait_RunCodeText(t *testing.T) {
	t.Parallel()
	responses := []json.RawMessage{
		json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`),
	}
	b := newMCPBackendWithClient(t, responses, nil)
	err := b.Wait(context.Background(), nil, WaitOptions{Text: "ready"})
	require.NoError(t, err)
}

// -------- MCPBackend.Close happy-path --------

// TestMCPClose_WithTool covers the browser_close tool branch.
func TestMCPClose_WithTool(t *testing.T) {
	t.Parallel()
	responses := []json.RawMessage{
		json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`),
	}
	b := newMCPBackendWithClient(t, responses, map[string]bool{"browser_close": true})
	err := b.Close(context.Background(), nil)
	require.NoError(t, err)
}

// -------- MCPBackend.call error paths --------

// TestMCPCall_ToolReturnedErrorWithText covers the "returned an error: <text>"
// formatting branch.
func TestMCPCall_ToolReturnedErrorWithText(t *testing.T) {
	t.Parallel()
	responses := []json.RawMessage{
		json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"detail"}],"isError":true}}`),
	}
	b := newMCPBackendWithClient(t, responses, nil)
	_, err := b.call(context.Background(), "browser_navigate", map[string]any{"url": "https://x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "detail")
}

// TestMCPCall_ToolReturnedErrorWithoutText covers the branch with no text blocks.
func TestMCPCall_ToolReturnedErrorWithoutText(t *testing.T) {
	t.Parallel()
	responses := []json.RawMessage{
		json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"content":[],"isError":true}}`),
	}
	b := newMCPBackendWithClient(t, responses, nil)
	_, err := b.call(context.Background(), "browser_navigate", map[string]any{"url": "https://x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "returned an error")
}

// TestMCPCall_SendError covers the CallTool-failure branch (transport EOF).
func TestMCPCall_SendError(t *testing.T) {
	t.Parallel()
	// No responses — Receive returns io.EOF.
	b := newMCPBackendWithClient(t, nil, nil)
	_, err := b.call(context.Background(), "browser_navigate", map[string]any{"url": "https://x"})
	require.Error(t, err)
}

// -------- Snapshot tool branch via MCP Open using browser_snapshot tool --------

// TestMCPOpen_WithSnapshotTool covers Open using browser_snapshot in Snapshot.
func TestMCPOpen_WithSnapshotTool(t *testing.T) {
	t.Parallel()
	responses := []json.RawMessage{
		// browser_navigate
		json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`),
		// browser_snapshot
		json.RawMessage(`{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"title: Hello\nurl: /"}]}}`),
	}
	b := newMCPBackendWithClient(t, responses, map[string]bool{"browser_snapshot": true})
	_, result, err := b.Open(context.Background(), nil, OpenOptions{URL: "https://example.com"})
	require.NoError(t, err)
	assert.Equal(t, "Hello", result.Title)
}

// -------- ensureUniqueSelector exercised via browser_test.go integration tests
// already rely on real Chrome. Add a small unit test that covers the
// JSON-marshal path without invoking Chrome by asserting nothing panics
// when called without a valid context. --------

// TestEnsureUniqueSelector_BadContext covers the chromedp-call error branch.
func TestEnsureUniqueSelector_BadContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := ensureUniqueSelector(ctx, "#btn")
	require.Error(t, err)
}

// -------- SearchHint coverage --------

// TestSearchHint verifies the tool.SearchHint metadata accessor.
func TestSearchHint(t *testing.T) {
	t.Parallel()
	tl := &tool{searchHint: "hint"}
	assert.Equal(t, "hint", tl.SearchHint())
}

// -------- Native Screenshot error path (writes to a read-only path) --------

// TestNativeBackendScreenshot_WriteError covers the os.WriteFile error branch
// by passing a path inside a read-only directory that does not exist.
// We call Screenshot with a nil handle first — that returns requireNativeSession
// error, so instead we construct a session and cancel it to fail the chromedp
// screenshot. This is already covered by the integration test; keep this as
// a unit test ensuring errResult helper formats correctly.
func TestErrResultFormatting(t *testing.T) {
	t.Parallel()
	r := errResult("failed: %s", "x")
	assert.True(t, r.IsError)
	assert.Contains(t, r.Content, "failed: x")
}

// -------- Additional MCP client fallback paths --------

// TestMCPBackend_FirstSnapshotLineEdgeCases covers edge cases not covered elsewhere.
func TestFirstSnapshotLineNewlineOnly(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", firstSnapshotLine("\n\n", "title: "))
}

// -------- Screenshot path edge cases --------

// TestScreenshotPathCreatesArtifactDir verifies path sanitization is stable.
func TestScreenshotPathDifferentSessions(t *testing.T) {
	t.Parallel()
	svc := &Service{artifactDir: t.TempDir()}
	p1 := svc.screenshotPath("alpha")
	p2 := svc.screenshotPath("beta")
	assert.NotEqual(t, p1, p2)
	assert.True(t, filepath.IsAbs(p1))
}

// -------- NewService error: artifact dir collides with a file --------

// TestNewServiceArtifactDirIsFile covers the MkdirAll error branch when
// the path already exists as a regular file.
func TestNewServiceArtifactDirIsFile(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	// Place a regular file where the default artifact dir would go.
	defaultDir := filepath.Join(workDir, ".rubichan")
	require.NoError(t, os.MkdirAll(defaultDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(defaultDir, "browser"), []byte("not a dir"), 0o644))

	_, err := NewService(workDir, config.BrowserConfig{}, nil)
	require.Error(t, err)
}
