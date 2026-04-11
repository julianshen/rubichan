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

func TestCmuxSplitTool_Execute(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	mc.SetResult("surface.split", cmux.Surface{ID: "new-surf", Type: "terminal"})

	tool := NewCmuxSplit(mc)
	input, _ := json.Marshal(map[string]string{"direction": "right"})
	result, err := tool.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.False(t, result.IsError, result.Content)
	assert.Contains(t, result.Content, "right")
	assert.Contains(t, result.Content, "new-surf")

	calls := mc.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "surface.split", calls[0].Method)
}

func TestCmuxSendTool_Text(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	mc.SetResult("surface.send_text", map[string]any{})

	tool := NewCmuxSend(mc)
	input, _ := json.Marshal(map[string]string{"surface_id": "surf-1", "text": "hello"})
	result, err := tool.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.False(t, result.IsError, result.Content)
	assert.Contains(t, result.Content, "surf-1")

	calls := mc.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "surface.send_text", calls[0].Method)
}

func TestCmuxSendTool_Key(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	mc.SetResult("surface.send_key", map[string]any{})

	tool := NewCmuxSend(mc)
	input, _ := json.Marshal(map[string]string{"surface_id": "surf-2", "key": "Enter"})
	result, err := tool.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.False(t, result.IsError, result.Content)
	assert.Contains(t, result.Content, "Enter")
	assert.Contains(t, result.Content, "surf-2")

	calls := mc.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "surface.send_key", calls[0].Method)
}

func TestCmuxSendTool_NeitherTextNorKey(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	tool := NewCmuxSend(mc)
	input, _ := json.Marshal(map[string]string{"surface_id": "surf-3"})
	result, err := tool.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "either text or key")
}

func TestCmuxSendTool_BothTextAndKey(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	tool := NewCmuxSend(mc)
	input, _ := json.Marshal(map[string]string{"surface_id": "surf-4", "text": "hi", "key": "Enter"})
	result, err := tool.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "mutually exclusive")
}
