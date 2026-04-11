package cmuxtest_test

import (
	"testing"

	"github.com/julianshen/rubichan/internal/cmux/cmuxtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockClient_RecordsCalls(t *testing.T) {
	m := cmuxtest.NewMockClient()
	m.SetResult("workspace.list", map[string]any{"items": []string{"ws-1"}})

	resp, err := m.Call("workspace.list", map[string]string{})
	require.NoError(t, err)
	assert.True(t, resp.OK)

	calls := m.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "workspace.list", calls[0].Method)
}

func TestMockClient_UnknownMethodErrors(t *testing.T) {
	m := cmuxtest.NewMockClient()

	_, err := m.Call("unknown.method", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown.method")
}

func TestMockClient_Identity(t *testing.T) {
	m := cmuxtest.NewMockClient()

	id := m.Identity()
	require.NotNil(t, id)
	assert.Equal(t, "mock-window", id.WindowID)
	assert.Equal(t, "mock-workspace", id.WorkspaceID)
	assert.Equal(t, "mock-pane", id.PaneID)
	assert.Equal(t, "mock-surface", id.SurfaceID)
}

func TestMockClient_SetError(t *testing.T) {
	m := cmuxtest.NewMockClient()
	m.SetError("workspace.list", "permission denied")

	resp, err := m.Call("workspace.list", nil)
	require.NoError(t, err) // no transport error
	assert.False(t, resp.OK)
	assert.Equal(t, "permission denied", resp.Error)
}

func TestMockClient_SetErrorThenSetResult(t *testing.T) {
	m := cmuxtest.NewMockClient()
	m.SetError("foo", "bad")
	m.SetResult("foo", "good")

	resp, err := m.Call("foo", nil)
	require.NoError(t, err)
	assert.True(t, resp.OK)
}

func TestMockClient_MarshalError(t *testing.T) {
	m := cmuxtest.NewMockClient()
	// Functions cannot be marshalled to JSON.
	m.SetResult("bad", func() {})

	_, err := m.Call("bad", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal")
}

func TestMockClient_Reset(t *testing.T) {
	m := cmuxtest.NewMockClient()
	m.SetResult("foo", "bar")
	_, _ = m.Call("foo", nil)

	m.Reset()
	assert.Empty(t, m.Calls())

	_, err := m.Call("foo", nil)
	require.Error(t, err) // result was cleared
}
