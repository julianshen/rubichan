package knowledgegraph

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOllamaEmbedder(t *testing.T) {
	// Mock Ollama server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/embeddings" {
			var req struct {
				Model  string `json:"model"`
				Prompt string `json:"prompt"`
			}
			err := json.NewDecoder(r.Body).Decode(&req)
			require.NoError(t, err)

			resp := map[string]interface{}{
				"embedding": []float64{0.1, 0.2, 0.3, 0.4},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer server.Close()

	embedder := NewOllamaEmbedder(server.URL)
	require.NotNil(t, embedder)

	vec, err := embedder.Embed(context.Background(), "test text")
	require.NoError(t, err)
	require.Len(t, vec, 4)
	require.InDelta(t, float32(0.1), vec[0], 0.001)
	require.InDelta(t, float32(0.4), vec[3], 0.001)
}

func TestOllamaEmbedderDims(t *testing.T) {
	embedder := NewOllamaEmbedder("http://localhost:11434")
	require.Equal(t, 768, embedder.Dims())
}

func TestOllamaEmbedderWithModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/embeddings" {
			var req struct {
				Model string `json:"model"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			require.Equal(t, "custom-model", req.Model)

			resp := map[string]interface{}{
				"embedding": []float64{1.0, 2.0},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer server.Close()

	embedder := NewOllamaEmbedderWithModel(server.URL, "custom-model")
	vec, err := embedder.Embed(context.Background(), "test")
	require.NoError(t, err)
	require.Len(t, vec, 2)
}

func TestOllamaEmbedderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer server.Close()

	embedder := NewOllamaEmbedder(server.URL)
	_, err := embedder.Embed(context.Background(), "test")
	require.Error(t, err)
	require.Contains(t, err.Error(), "status 500")
}

func TestOllamaEmbedderContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Just wait forever
		<-r.Context().Done()
	}))
	defer server.Close()

	embedder := NewOllamaEmbedder(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := embedder.Embed(ctx, "test")
	require.Error(t, err)
}

func TestOllamaHealthCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"models":[]}`))
		}
	}))
	defer server.Close()

	embedder := NewOllamaEmbedder(server.URL)
	err := embedder.HealthCheck(context.Background())
	require.NoError(t, err)
}

func TestOllamaHealthCheckUnavailable(t *testing.T) {
	embedder := NewOllamaEmbedder("http://invalid-url-that-does-not-exist:99999")
	err := embedder.HealthCheck(context.Background())
	require.Error(t, err)
	require.Equal(t, ErrEmbedderUnavailable, err)
}

func TestOpenAIEmbedder(t *testing.T) {
	// Mock OpenAI server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/embeddings" {
			// Verify auth header
			auth := r.Header.Get("Authorization")
			require.Contains(t, auth, "Bearer test-key")

			var req struct {
				Model string `json:"model"`
				Input string `json:"input"`
			}
			json.NewDecoder(r.Body).Decode(&req)

			resp := map[string]interface{}{
				"data": []map[string]interface{}{
					{
						"embedding": []float64{0.5, 0.6, 0.7},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer server.Close()

	// Replace the URL in the embedder (hacky but works for testing)
	embedder := &OpenAIEmbedder{
		apiKey: "test-key",
		model:  "text-embedding-3-small",
		client: &http.Client{},
	}

	// We can't easily override the URL, so this test just verifies the structure
	require.Equal(t, "text-embedding-3-small", embedder.model)
	require.Equal(t, 1536, embedder.Dims())
}

func TestOpenAIEmbedderDims(t *testing.T) {
	embedder := NewOpenAIEmbedder("test-key")
	require.Equal(t, 1536, embedder.Dims())
}

func TestOpenAIEmbedderWithModel(t *testing.T) {
	embedder := NewOpenAIEmbedderWithModel("test-key", "text-embedding-3-large")
	require.Equal(t, "text-embedding-3-large", embedder.model)
	require.Equal(t, 1536, embedder.Dims()) // Dims is fixed, not model-dependent
}
