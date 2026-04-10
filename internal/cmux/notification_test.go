package cmux_test

import (
	"testing"

	"github.com/julianshen/rubichan/internal/cmux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotify(t *testing.T) {
	handlers := defaultHandlers()
	var capturedTitle, capturedSubtitle, capturedBody string
	handlers["notification.create"] = func(req jsonrpcRequest) interface{} {
		var p struct {
			Title    string `json:"title"`
			Subtitle string `json:"subtitle"`
			Body     string `json:"body"`
		}
		_ = unmarshalParams(req, &p)
		capturedTitle = p.Title
		capturedSubtitle = p.Subtitle
		capturedBody = p.Body
		return map[string]string{"id": "notif-1"}
	}
	socketPath := newTestServer(t, handlers)
	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	err = c.Notify("Build done", "CI passed", "All 42 tests passed.")
	require.NoError(t, err)
	assert.Equal(t, "Build done", capturedTitle)
	assert.Equal(t, "CI passed", capturedSubtitle)
	assert.Equal(t, "All 42 tests passed.", capturedBody)
}

func TestListNotifications(t *testing.T) {
	handlers := defaultHandlers()
	handlers["notification.list"] = func(req jsonrpcRequest) interface{} {
		return []map[string]string{
			{"id": "n1", "title": "Hello", "subtitle": "Sub", "body": "World"},
			{"id": "n2", "title": "Alert", "subtitle": "", "body": "Disk full"},
		}
	}
	socketPath := newTestServer(t, handlers)
	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	notifications, err := c.ListNotifications()
	require.NoError(t, err)
	require.Len(t, notifications, 2)
	assert.Equal(t, "n1", notifications[0].ID)
	assert.Equal(t, "Hello", notifications[0].Title)
	assert.Equal(t, "Sub", notifications[0].Subtitle)
	assert.Equal(t, "World", notifications[0].Body)
	assert.Equal(t, "n2", notifications[1].ID)
}

func TestClearNotifications(t *testing.T) {
	handlers := defaultHandlers()
	cleared := false
	handlers["notification.clear"] = func(req jsonrpcRequest) interface{} {
		cleared = true
		return map[string]string{}
	}
	socketPath := newTestServer(t, handlers)
	c, err := cmux.Dial(socketPath)
	require.NoError(t, err)
	defer c.Close()

	err = c.ClearNotifications()
	require.NoError(t, err)
	assert.True(t, cleared)
}
