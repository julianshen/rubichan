package agentsdk

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUIRequestFuncAdapter(t *testing.T) {
	handler := UIRequestFunc(func(_ context.Context, req UIRequest) (UIResponse, error) {
		assert.Equal(t, "req_1", req.ID)
		assert.Equal(t, UIKindSelect, req.Kind)
		return UIResponse{
			RequestID: req.ID,
			ActionID:  "pick",
			Values:    json.RawMessage(`{"option":"A"}`),
		}, nil
	})

	resp, err := handler.Request(context.Background(), UIRequest{
		ID:   "req_1",
		Kind: UIKindSelect,
	})
	require.NoError(t, err)
	assert.Equal(t, "req_1", resp.RequestID)
	assert.Equal(t, "pick", resp.ActionID)
	assert.JSONEq(t, `{"option":"A"}`, string(resp.Values))
}

func TestUIRequestFuncNilGuard(t *testing.T) {
	var handler UIRequestFunc
	_, err := handler.Request(context.Background(), UIRequest{ID: "req_nil"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil UIRequestFunc")
}
