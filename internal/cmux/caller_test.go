package cmux_test

import (
	"testing"

	"github.com/julianshen/rubichan/internal/cmux"
	"github.com/julianshen/rubichan/internal/cmux/cmuxtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCallerNotify(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	mc.SetResult("notification.create", true)

	ok := cmux.CallerNotify(mc, "Test", "Sub", "Body text")
	assert.True(t, ok)

	calls := mc.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "notification.create", calls[0].Method)
	params, paramsOK := calls[0].Params.(map[string]string)
	require.True(t, paramsOK)
	assert.Equal(t, "Test", params["title"])
	assert.Equal(t, "Sub", params["subtitle"])
	assert.Equal(t, "Body text", params["body"])
}

func TestCallerNotifyFailure(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	mc.SetError("notification.create", "not allowed")

	ok := cmux.CallerNotify(mc, "Test", "Sub", "Body text")
	assert.False(t, ok)
}

func TestCallerSetProgress(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	mc.SetResult("set-progress", true)

	cmux.CallerSetProgress(mc, 0.75, "Analyzing...")

	calls := mc.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "set-progress", calls[0].Method)
	params, ok := calls[0].Params.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 0.75, params["value"])
	assert.Equal(t, "Analyzing...", params["label"])
}

func TestCallerClearProgress(t *testing.T) {
	mc := cmuxtest.NewMockClient()
	mc.SetResult("clear-progress", true)

	cmux.CallerClearProgress(mc)

	calls := mc.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "clear-progress", calls[0].Method)
}
