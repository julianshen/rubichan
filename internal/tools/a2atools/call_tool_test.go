package a2atools

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/julianshen/rubichan/internal/a2a"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCallToolSyncCompletion(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var req a2a.RPCRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		result, _ := json.Marshal(a2a.Task{
			ID: "task-1", State: a2a.TaskCompleted,
			Artifacts: []a2a.Artifact{{Name: "result", Parts: []a2a.Part{{Kind: "text", Text: "42"}}}},
		})
		resp := a2a.RPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	cfg := config.DefaultConfig()
	tool := NewCallTool(cfg, s)
	tool.httpClient = srv.Client()

	input, err := json.Marshal(map[string]string{"agent": srv.URL, "message": "what is 6*7?"})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "42")
}
