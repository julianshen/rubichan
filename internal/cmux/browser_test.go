package cmux_test

import (
	"testing"

	"github.com/julianshen/rubichan/internal/cmux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrowserNavigate(t *testing.T) {
	handlers := defaultHandlers()
	var capturedSurfaceID, capturedURL string
	handlers["browser.navigate"] = func(req jsonrpcRequest) interface{} {
		var p struct {
			SurfaceID string `json:"surface_id"`
			URL       string `json:"url"`
		}
		_ = unmarshalParams(req, &p)
		capturedSurfaceID = p.SurfaceID
		capturedURL = p.URL
		return map[string]string{}
	}
	socketPath := newTestServer(t, handlers)
	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	err = c.BrowserNavigate("surf-1", "https://example.com")
	require.NoError(t, err)
	assert.Equal(t, "surf-1", capturedSurfaceID)
	assert.Equal(t, "https://example.com", capturedURL)
}

func TestBrowserSnapshot(t *testing.T) {
	handlers := defaultHandlers()
	var capturedSurfaceID string
	handlers["browser.snapshot"] = func(req jsonrpcRequest) interface{} {
		var p struct {
			SurfaceID string `json:"surface_id"`
		}
		_ = unmarshalParams(req, &p)
		capturedSurfaceID = p.SurfaceID
		return map[string]string{"dom": "<html><body>Hello</body></html>"}
	}
	socketPath := newTestServer(t, handlers)
	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	dom, err := c.BrowserSnapshot("surf-1")
	require.NoError(t, err)
	assert.Equal(t, "<html><body>Hello</body></html>", dom)
	assert.Equal(t, "surf-1", capturedSurfaceID)
}

func TestBrowserClick(t *testing.T) {
	handlers := defaultHandlers()
	var capturedSurfaceID, capturedRef string
	handlers["browser.click"] = func(req jsonrpcRequest) interface{} {
		var p struct {
			SurfaceID string `json:"surface_id"`
			Ref       string `json:"ref"`
		}
		_ = unmarshalParams(req, &p)
		capturedSurfaceID = p.SurfaceID
		capturedRef = p.Ref
		return map[string]string{}
	}
	socketPath := newTestServer(t, handlers)
	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	err = c.BrowserClick("surf-1", "#submit-btn")
	require.NoError(t, err)
	assert.Equal(t, "surf-1", capturedSurfaceID)
	assert.Equal(t, "#submit-btn", capturedRef)
}

func TestBrowserType(t *testing.T) {
	handlers := defaultHandlers()
	var capturedSurfaceID, capturedRef, capturedText string
	handlers["browser.type"] = func(req jsonrpcRequest) interface{} {
		var p struct {
			SurfaceID string `json:"surface_id"`
			Ref       string `json:"ref"`
			Text      string `json:"text"`
		}
		_ = unmarshalParams(req, &p)
		capturedSurfaceID = p.SurfaceID
		capturedRef = p.Ref
		capturedText = p.Text
		return map[string]string{}
	}
	socketPath := newTestServer(t, handlers)
	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	err = c.BrowserType("surf-1", "#search-input", "rubichan")
	require.NoError(t, err)
	assert.Equal(t, "surf-1", capturedSurfaceID)
	assert.Equal(t, "#search-input", capturedRef)
	assert.Equal(t, "rubichan", capturedText)
}

func TestBrowserWait(t *testing.T) {
	handlers := defaultHandlers()
	var capturedSurfaceID, capturedLoadState string
	handlers["browser.wait"] = func(req jsonrpcRequest) interface{} {
		var p struct {
			SurfaceID string `json:"surface_id"`
			LoadState string `json:"load_state"`
		}
		_ = unmarshalParams(req, &p)
		capturedSurfaceID = p.SurfaceID
		capturedLoadState = p.LoadState
		return map[string]string{}
	}
	socketPath := newTestServer(t, handlers)
	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	err = c.BrowserWait("surf-1", "networkidle")
	require.NoError(t, err)
	assert.Equal(t, "surf-1", capturedSurfaceID)
	assert.Equal(t, "networkidle", capturedLoadState)
}
