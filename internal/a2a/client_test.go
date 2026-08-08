package a2a

import (
	"context"
	"net/http"
	"testing"

	"github.com/julianshen/rubichan/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientDiscover(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"name": "test-agent",
			"description": "a test agent",
			"url": "` + r.Host + `",
			"skills": [{"id": "echo", "name": "Echo", "description": "echoes input"}],
			"capabilities": {"streaming": true}
		}`))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	client := NewClient(srv.URL, "", srv.Client())
	card, err := client.Discover(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "test-agent", card.Name)
	assert.Equal(t, "echo", card.Skills[0].ID)
	assert.True(t, card.Capabilities.Streaming)
}

func TestClientDiscoverNotFound(t *testing.T) {
	handler := http.NewServeMux()
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	client := NewClient(srv.URL, "", srv.Client())
	_, err := client.Discover(context.Background())
	assert.Error(t, err)
}

func TestClientDiscoverWithAPIKey(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		// Verify the Bearer token was sent
		assert.Equal(t, "Bearer test-api-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"name": "secure-agent",
			"description": "a secure agent",
			"url": "` + r.Host + `",
			"skills": [],
			"capabilities": {}
		}`))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	client := NewClient(srv.URL, "test-api-key", srv.Client())
	card, err := client.Discover(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "secure-agent", card.Name)
}

func TestClientDiscoverWithNilHTTPClient(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"name": "test-agent",
			"description": "a test agent",
			"url": "` + r.Host + `",
			"skills": [],
			"capabilities": {}
		}`))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	client := NewClient(srv.URL, "", nil)
	card, err := client.Discover(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "test-agent", card.Name)
}

func TestClientDiscoverInvalidJSON(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`invalid json`))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	client := NewClient(srv.URL, "", srv.Client())
	_, err := client.Discover(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse agent card")
}

func TestClientDiscoverCanceled(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/.well-known/agent.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"name": "test-agent",
			"description": "a test agent",
			"url": "` + r.Host + `",
			"skills": [],
			"capabilities": {}
		}`))
	})
	srv := testutil.NewServer(t, handler)
	defer srv.Close()

	client := NewClient(srv.URL, "", srv.Client())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Discover(ctx)
	assert.Error(t, err)
}
