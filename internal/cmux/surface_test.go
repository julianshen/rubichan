package cmux_test

import (
	"testing"

	"github.com/julianshen/rubichan/internal/cmux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplit(t *testing.T) {
	handlers := defaultHandlers()
	var capturedDirection string
	handlers["surface.split"] = func(req jsonrpcRequest) interface{} {
		var p struct {
			Direction string `json:"direction"`
		}
		_ = unmarshalParams(req, &p)
		capturedDirection = p.Direction
		return map[string]string{"id": "surf-new", "type": "pane"}
	}
	socketPath := newTestServer(t, handlers)
	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	surf, err := c.Split("horizontal")
	require.NoError(t, err)
	require.NotNil(t, surf)
	assert.Equal(t, "surf-new", surf.ID)
	assert.Equal(t, "pane", surf.Type)
	assert.Equal(t, "horizontal", capturedDirection)
}

func TestListSurfaces(t *testing.T) {
	handlers := defaultHandlers()
	handlers["surface.list"] = func(req jsonrpcRequest) interface{} {
		return map[string]interface{}{
			"surfaces": []map[string]string{
				{"id": "surf-1", "type": "pane"},
				{"id": "surf-2", "type": "browser"},
			},
		}
	}
	socketPath := newTestServer(t, handlers)
	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	surfaces, err := c.ListSurfaces()
	require.NoError(t, err)
	require.Len(t, surfaces, 2)
	assert.Equal(t, "surf-1", surfaces[0].ID)
	assert.Equal(t, "pane", surfaces[0].Type)
	assert.Equal(t, "surf-2", surfaces[1].ID)
	assert.Equal(t, "browser", surfaces[1].Type)
}

func TestSplitError(t *testing.T) {
	socketPath := newErrorServer(t, "surface.split")
	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	_, err = c.Split("right")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "surface.split")
}

func TestListSurfacesError(t *testing.T) {
	socketPath := newErrorServer(t, "surface.list")
	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	_, err = c.ListSurfaces()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "surface.list")
}

func TestFocusSurface(t *testing.T) {
	handlers := defaultHandlers()
	var capturedID string
	handlers["surface.focus"] = func(req jsonrpcRequest) interface{} {
		var p struct {
			SurfaceID string `json:"surface_id"`
		}
		_ = unmarshalParams(req, &p)
		capturedID = p.SurfaceID
		return map[string]string{}
	}
	socketPath := newTestServer(t, handlers)
	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	err = c.FocusSurface("surf-3")
	require.NoError(t, err)
	assert.Equal(t, "surf-3", capturedID)
}

func TestSendText(t *testing.T) {
	handlers := defaultHandlers()
	var capturedSurfaceID, capturedText string
	handlers["surface.send_text"] = func(req jsonrpcRequest) interface{} {
		var p struct {
			SurfaceID string `json:"surface_id"`
			Text      string `json:"text"`
		}
		_ = unmarshalParams(req, &p)
		capturedSurfaceID = p.SurfaceID
		capturedText = p.Text
		return map[string]string{}
	}
	socketPath := newTestServer(t, handlers)
	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	err = c.SendText("surf-3", "hello world")
	require.NoError(t, err)
	assert.Equal(t, "surf-3", capturedSurfaceID)
	assert.Equal(t, "hello world", capturedText)
}

func TestSendKey(t *testing.T) {
	handlers := defaultHandlers()
	var capturedSurfaceID, capturedKey string
	handlers["surface.send_key"] = func(req jsonrpcRequest) interface{} {
		var p struct {
			SurfaceID string `json:"surface_id"`
			Key       string `json:"key"`
		}
		_ = unmarshalParams(req, &p)
		capturedSurfaceID = p.SurfaceID
		capturedKey = p.Key
		return map[string]string{}
	}
	socketPath := newTestServer(t, handlers)
	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	err = c.SendKey("surf-3", "ctrl+c")
	require.NoError(t, err)
	assert.Equal(t, "surf-3", capturedSurfaceID)
	assert.Equal(t, "ctrl+c", capturedKey)
}
