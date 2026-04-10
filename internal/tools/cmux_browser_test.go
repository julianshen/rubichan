package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/cmux"
	"github.com/julianshen/rubichan/internal/cmux/cmuxtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCmuxBrowserNavigateTool_Name(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	tool := NewCmuxBrowserNavigate(mc)
	assert.Equal(t, "cmux_browser_navigate", tool.Name())
}

func TestCmuxBrowserNavigateTool_Execute(t *testing.T) {
	// no surface_id → auto-split
	mc := cmuxtest.NewMockClient()
	mc.SetResult("surface.split", cmux.Surface{ID: "surf-1", Type: "browser"})
	mc.SetResult("browser.navigate", map[string]any{})
	mc.SetResult("browser.wait", map[string]any{})

	tool := NewCmuxBrowserNavigate(mc)
	input, _ := json.Marshal(map[string]string{"url": "https://example.com"})
	result, err := tool.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.False(t, result.IsError, result.Content)
	assert.Contains(t, result.Content, "https://example.com")
	assert.Contains(t, result.Content, "surf-1")

	calls := mc.Calls()
	require.Len(t, calls, 3)
	assert.Equal(t, "surface.split", calls[0].Method)
	assert.Equal(t, "browser.navigate", calls[1].Method)
	assert.Equal(t, "browser.wait", calls[2].Method)
}

func TestCmuxBrowserNavigateTool_WithSurfaceID(t *testing.T) {
	// surface_id provided → no split call
	mc := cmuxtest.NewMockClient()
	mc.SetResult("browser.navigate", map[string]any{})
	mc.SetResult("browser.wait", map[string]any{})

	tool := NewCmuxBrowserNavigate(mc)
	input, _ := json.Marshal(map[string]string{"url": "https://example.com", "surface_id": "existing-surf"})
	result, err := tool.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.False(t, result.IsError, result.Content)
	assert.Contains(t, result.Content, "existing-surf")

	calls := mc.Calls()
	require.Len(t, calls, 2)
	assert.Equal(t, "browser.navigate", calls[0].Method)
	assert.Equal(t, "browser.wait", calls[1].Method)
}

func TestCmuxBrowserSnapshotTool_Execute(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	mc.SetResult("browser.snapshot", map[string]string{"dom": "<html>hello</html>"})

	tool := NewCmuxBrowserSnapshot(mc)
	input, _ := json.Marshal(map[string]string{"surface_id": "surf-2"})
	result, err := tool.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.False(t, result.IsError, result.Content)
	assert.Equal(t, "<html>hello</html>", result.Content)
}

func TestCmuxBrowserClickTool_Execute(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	mc.SetResult("browser.click", map[string]any{})

	tool := NewCmuxBrowserClick(mc)
	input, _ := json.Marshal(map[string]string{"surface_id": "surf-3", "ref": "button#submit"})
	result, err := tool.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.False(t, result.IsError, result.Content)
	assert.Contains(t, result.Content, "button#submit")
	assert.Contains(t, result.Content, "surf-3")
}

func TestCmuxBrowserTypeTool_Execute(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	mc.SetResult("browser.type", map[string]any{})

	tool := NewCmuxBrowserType(mc)
	input, _ := json.Marshal(map[string]string{"surface_id": "surf-4", "ref": "input#search", "text": "hello world"})
	result, err := tool.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.False(t, result.IsError, result.Content)
	assert.Contains(t, result.Content, "hello world")
	assert.Contains(t, result.Content, "surf-4")
}

func TestCmuxBrowserWaitTool_Execute(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	mc.SetResult("browser.wait", map[string]any{})

	tool := NewCmuxBrowserWait(mc)
	input, _ := json.Marshal(map[string]string{"surface_id": "surf-5", "load_state": "networkidle"})
	result, err := tool.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.False(t, result.IsError, result.Content)
	assert.Contains(t, result.Content, "surf-5")
	assert.Contains(t, result.Content, "networkidle")
}
