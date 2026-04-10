package cmux_test

import (
	"testing"

	"github.com/julianshen/rubichan/internal/cmux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListWorkspaces(t *testing.T) {
	handlers := defaultHandlers()
	handlers["workspace.list"] = func(req jsonrpcRequest) interface{} {
		return map[string]interface{}{
			"workspaces": []map[string]string{
				{"id": "ws-1", "name": "alpha"},
				{"id": "ws-2", "name": "beta"},
			},
		}
	}
	socketPath := newTestServer(t, handlers)
	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	workspaces, err := c.ListWorkspaces()
	require.NoError(t, err)
	require.Len(t, workspaces, 2)
	assert.Equal(t, "ws-1", workspaces[0].ID)
	assert.Equal(t, "alpha", workspaces[0].Name)
	assert.Equal(t, "ws-2", workspaces[1].ID)
	assert.Equal(t, "beta", workspaces[1].Name)
}

func TestCreateWorkspace(t *testing.T) {
	handlers := defaultHandlers()
	handlers["workspace.create"] = func(req jsonrpcRequest) interface{} {
		return map[string]string{"id": "ws-new", "name": "new-workspace"}
	}
	socketPath := newTestServer(t, handlers)
	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	ws, err := c.CreateWorkspace()
	require.NoError(t, err)
	require.NotNil(t, ws)
	assert.Equal(t, "ws-new", ws.ID)
	assert.Equal(t, "new-workspace", ws.Name)
}

func TestSelectWorkspace(t *testing.T) {
	handlers := defaultHandlers()
	var capturedID string
	handlers["workspace.select"] = func(req jsonrpcRequest) interface{} {
		var p struct {
			WorkspaceID string `json:"workspace_id"`
		}
		_ = unmarshalParams(req, &p)
		capturedID = p.WorkspaceID
		return map[string]string{}
	}
	socketPath := newTestServer(t, handlers)
	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	err = c.SelectWorkspace("ws-42")
	require.NoError(t, err)
	assert.Equal(t, "ws-42", capturedID)
}

func TestCurrentWorkspace(t *testing.T) {
	handlers := defaultHandlers()
	handlers["workspace.current"] = func(req jsonrpcRequest) interface{} {
		return map[string]string{"id": "ws-42", "name": "current"}
	}
	socketPath := newTestServer(t, handlers)
	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	ws, err := c.CurrentWorkspace()
	require.NoError(t, err)
	require.NotNil(t, ws)
	assert.Equal(t, "ws-42", ws.ID)
	assert.Equal(t, "current", ws.Name)
}

func TestCloseWorkspace(t *testing.T) {
	handlers := defaultHandlers()
	var capturedID string
	handlers["workspace.close"] = func(req jsonrpcRequest) interface{} {
		var p struct {
			WorkspaceID string `json:"workspace_id"`
		}
		_ = unmarshalParams(req, &p)
		capturedID = p.WorkspaceID
		return map[string]string{}
	}
	socketPath := newTestServer(t, handlers)
	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	err = c.CloseWorkspace("ws-42")
	require.NoError(t, err)
	assert.Equal(t, "ws-42", capturedID)
}
