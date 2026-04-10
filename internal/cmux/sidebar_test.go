package cmux_test

import (
	"testing"

	"github.com/julianshen/rubichan/internal/cmux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetStatus(t *testing.T) {
	handlers := defaultHandlers()
	var capturedKey, capturedValue, capturedIcon, capturedColor string
	handlers["set-status"] = func(req jsonrpcRequest) interface{} {
		var p struct {
			Key   string `json:"key"`
			Value string `json:"value"`
			Icon  string `json:"icon"`
			Color string `json:"color"`
		}
		_ = unmarshalParams(req, &p)
		capturedKey = p.Key
		capturedValue = p.Value
		capturedIcon = p.Icon
		capturedColor = p.Color
		return map[string]string{"ok": "true"}
	}
	socketPath := newTestServer(t, handlers)
	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	err = c.SetStatus("build", "passing", "✓", "green")
	require.NoError(t, err)
	assert.Equal(t, "build", capturedKey)
	assert.Equal(t, "passing", capturedValue)
	assert.Equal(t, "✓", capturedIcon)
	assert.Equal(t, "green", capturedColor)
}

func TestClearStatus(t *testing.T) {
	handlers := defaultHandlers()
	var capturedKey string
	handlers["clear-status"] = func(req jsonrpcRequest) interface{} {
		var p struct {
			Key string `json:"key"`
		}
		_ = unmarshalParams(req, &p)
		capturedKey = p.Key
		return map[string]string{}
	}
	socketPath := newTestServer(t, handlers)
	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	err = c.ClearStatus("build")
	require.NoError(t, err)
	assert.Equal(t, "build", capturedKey)
}

func TestSetProgress(t *testing.T) {
	handlers := defaultHandlers()
	var capturedFraction float64
	var capturedLabel string
	handlers["set-progress"] = func(req jsonrpcRequest) interface{} {
		var p struct {
			Value float64 `json:"value"`
			Label string  `json:"label"`
		}
		_ = unmarshalParams(req, &p)
		capturedFraction = p.Value
		capturedLabel = p.Label
		return map[string]string{}
	}
	socketPath := newTestServer(t, handlers)
	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	err = c.SetProgress(0.75, "Building…")
	require.NoError(t, err)
	assert.InDelta(t, 0.75, capturedFraction, 0.001)
	assert.Equal(t, "Building…", capturedLabel)
}

func TestClearProgress(t *testing.T) {
	handlers := defaultHandlers()
	handlers["clear-progress"] = func(req jsonrpcRequest) interface{} {
		return map[string]string{}
	}
	socketPath := newTestServer(t, handlers)
	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	err = c.ClearProgress()
	require.NoError(t, err)
}

func TestLog(t *testing.T) {
	handlers := defaultHandlers()
	var capturedMsg, capturedLevel, capturedSource string
	handlers["log"] = func(req jsonrpcRequest) interface{} {
		var p struct {
			Message string `json:"message"`
			Level   string `json:"level"`
			Source  string `json:"source"`
		}
		_ = unmarshalParams(req, &p)
		capturedMsg = p.Message
		capturedLevel = p.Level
		capturedSource = p.Source
		return map[string]string{}
	}
	socketPath := newTestServer(t, handlers)
	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	err = c.Log("build succeeded", "info", "agent")
	require.NoError(t, err)
	assert.Equal(t, "build succeeded", capturedMsg)
	assert.Equal(t, "info", capturedLevel)
	assert.Equal(t, "agent", capturedSource)
}

func TestClearLog(t *testing.T) {
	handlers := defaultHandlers()
	handlers["clear-log"] = func(req jsonrpcRequest) interface{} {
		return map[string]string{}
	}
	socketPath := newTestServer(t, handlers)
	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	err = c.ClearLog()
	require.NoError(t, err)
}

func TestSidebarState(t *testing.T) {
	handlers := defaultHandlers()
	handlers["sidebar-state"] = func(req jsonrpcRequest) interface{} {
		return map[string]interface{}{
			"cwd":        "/home/user/project",
			"git_branch": "main",
			"ports":      []int{8080, 9090},
			"status": []map[string]string{
				{"key": "ci", "value": "pass", "icon": "✓", "color": "green"},
			},
			"progress": map[string]interface{}{"value": 0.5, "label": "half"},
			"logs": []map[string]string{
				{"message": "done", "level": "info", "source": "agent"},
			},
		}
	}
	socketPath := newTestServer(t, handlers)
	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	state, err := c.SidebarState()
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "/home/user/project", state.CWD)
	assert.Equal(t, "main", state.GitBranch)
	assert.Equal(t, []int{8080, 9090}, state.Ports)
	require.Len(t, state.Status, 1)
	assert.Equal(t, "ci", state.Status[0].Key)
	require.NotNil(t, state.Progress)
	assert.InDelta(t, 0.5, state.Progress.Value, 0.001)
	require.Len(t, state.Logs, 1)
	assert.Equal(t, "done", state.Logs[0].Message)
}
