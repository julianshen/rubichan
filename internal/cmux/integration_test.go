//go:build cmux_integration

package cmux

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_Ping(t *testing.T) {
	if os.Getenv("CMUX_WORKSPACE_ID") == "" {
		t.Skip("not running inside cmux")
	}
	c, err := Dial(SocketPath())
	require.NoError(t, err)
	defer c.Close()

	assert.NotEmpty(t, c.Identity().WorkspaceID)
	assert.NotEmpty(t, c.Identity().SurfaceID)
}

func TestIntegration_Sidebar(t *testing.T) {
	if os.Getenv("CMUX_WORKSPACE_ID") == "" {
		t.Skip("not running inside cmux")
	}
	c, err := Dial(SocketPath())
	require.NoError(t, err)
	defer c.Close()

	require.NoError(t, c.SetStatus("test", "integration", "checkmark.circle", "#00ff00"))
	require.NoError(t, c.SetProgress(0.5, "Testing..."))
	require.NoError(t, c.Log("Integration test running", "info", "test"))

	state, err := c.SidebarState()
	require.NoError(t, err)
	assert.NotNil(t, state)

	require.NoError(t, c.ClearStatus("test"))
	require.NoError(t, c.ClearProgress())
	require.NoError(t, c.ClearLog())
}

func TestIntegration_Notify(t *testing.T) {
	if os.Getenv("CMUX_WORKSPACE_ID") == "" {
		t.Skip("not running inside cmux")
	}
	c, err := Dial(SocketPath())
	require.NoError(t, err)
	defer c.Close()

	require.NoError(t, c.Notify("Rubichan Test", "Integration", "This is a test notification"))
	require.NoError(t, c.ClearNotifications())
}

func TestIntegration_ListWorkspaces(t *testing.T) {
	if os.Getenv("CMUX_WORKSPACE_ID") == "" {
		t.Skip("not running inside cmux")
	}
	c, err := Dial(SocketPath())
	require.NoError(t, err)
	defer c.Close()

	workspaces, err := c.ListWorkspaces()
	require.NoError(t, err)
	assert.NotEmpty(t, workspaces)
}
