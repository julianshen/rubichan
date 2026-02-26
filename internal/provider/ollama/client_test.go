package ollama

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_ListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/tags", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"models": [
				{
					"name": "llama3.2:latest",
					"size": 4294967296,
					"modified_at": "2026-02-25T10:00:00Z",
					"digest": "sha256:abc123"
				},
				{
					"name": "codellama:7b",
					"size": 3758096384,
					"modified_at": "2026-02-20T08:00:00Z",
					"digest": "sha256:def456"
				}
			]
		}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	models, err := client.ListModels(context.Background())
	require.NoError(t, err)
	require.Len(t, models, 2)
	assert.Equal(t, "llama3.2:latest", models[0].Name)
	assert.Equal(t, int64(4294967296), models[0].Size)
	assert.Equal(t, "codellama:7b", models[1].Name)
}

func TestClient_ListModels_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"models": []}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	models, err := client.ListModels(context.Background())
	require.NoError(t, err)
	assert.Empty(t, models)
}

func TestClient_ListModels_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.ListModels(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")
}

func TestClient_ListModels_ConnectionError(t *testing.T) {
	client := NewClient("http://localhost:1") // nothing listening
	_, err := client.ListModels(context.Background())
	require.Error(t, err)
}
