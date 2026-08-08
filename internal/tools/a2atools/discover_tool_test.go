package a2atools

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverToolByRawURL(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"remote","description":"d","skills":[{"id":"echo","name":"Echo","description":"e"}]}`))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	tool := NewDiscoverTool(config.DefaultConfig())
	tool.httpClient = srv.Client()

	input, err := json.Marshal(map[string]string{"agent": srv.URL})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "remote")
	assert.Contains(t, result.Content, "echo")
}

func TestDiscoverToolByConfiguredName(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"research-bot","description":"d","skills":[]}`))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	cfg := config.DefaultConfig()
	cfg.A2A.Agents = []config.A2AAgentConfig{{Name: "research-bot", URL: srv.URL}}
	tool := NewDiscoverTool(cfg)
	tool.httpClient = srv.Client()

	input, err := json.Marshal(map[string]string{"agent": "research-bot"})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "research-bot")
}

func TestDiscoverToolUnknownName(t *testing.T) {
	tool := NewDiscoverTool(config.DefaultConfig())
	input, err := json.Marshal(map[string]string{"agent": "not-configured"})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestDiscoverToolName(t *testing.T) {
	tool := NewDiscoverTool(config.DefaultConfig())
	assert.Equal(t, "a2a_discover", tool.Name())
}

func TestDiscoverToolDescription(t *testing.T) {
	tool := NewDiscoverTool(config.DefaultConfig())
	desc := tool.Description()
	assert.NotEmpty(t, desc)
	assert.Contains(t, desc, "Agent2Agent")
}

func TestDiscoverToolInputSchema(t *testing.T) {
	tool := NewDiscoverTool(config.DefaultConfig())
	schema := tool.InputSchema()

	// Verify it's valid JSON
	var parsed interface{}
	assert.NoError(t, json.Unmarshal(schema, &parsed))

	// Verify it has the expected structure
	var schemaObj map[string]interface{}
	assert.NoError(t, json.Unmarshal(schema, &schemaObj))
	assert.Equal(t, "object", schemaObj["type"])
}

func TestDiscoverToolInvalidInput(t *testing.T) {
	tool := NewDiscoverTool(config.DefaultConfig())
	result, err := tool.Execute(context.Background(), []byte("invalid json"))
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid input")
}

func TestDiscoverToolDiscoverFails(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	tool := NewDiscoverTool(config.DefaultConfig())
	tool.httpClient = srv.Client()

	input, err := json.Marshal(map[string]string{"agent": srv.URL})
	require.NoError(t, err)

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "discover failed")
}
