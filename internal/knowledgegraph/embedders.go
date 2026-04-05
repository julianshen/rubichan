package knowledgegraph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// OllamaEmbedder generates embeddings using Ollama's embedding API.
// See: https://github.com/ollama/ollama/blob/main/docs/api.md#generate-embeddings
type OllamaEmbedder struct {
	baseURL string // e.g., "http://localhost:11434"
	model   string // e.g., "nomic-embed-text"
	client  *http.Client
}

// NewOllamaEmbedder creates an embedder using Ollama at the given base URL.
// Default model is "nomic-embed-text" (768 dimensions).
func NewOllamaEmbedder(baseURL string) *OllamaEmbedder {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &OllamaEmbedder{
		baseURL: baseURL,
		model:   "nomic-embed-text",
		client:  &http.Client{},
	}
}

// NewOllamaEmbedderWithModel creates an embedder using a specific Ollama model.
func NewOllamaEmbedderWithModel(baseURL, model string) *OllamaEmbedder {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &OllamaEmbedder{
		baseURL: baseURL,
		model:   model,
		client:  &http.Client{},
	}
}

// Embed generates a vector embedding for the given text.
func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	type request struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
	}
	type response struct {
		Embedding []float64 `json:"embedding"`
	}

	payload := request{Model: e.model, Prompt: text}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("Ollama: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", e.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("Ollama: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Ollama: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Ollama: status %d: %s", resp.StatusCode, string(respBody))
	}

	var result response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("Ollama: decode response: %w", err)
	}

	// Convert float64 to float32
	vec := make([]float32, len(result.Embedding))
	for i, v := range result.Embedding {
		vec[i] = float32(v)
	}

	return vec, nil
}

// Dims returns the dimensionality of nomic-embed-text (768) or other Ollama models.
// This is a fixed value; actual dimensionality depends on the model.
func (e *OllamaEmbedder) Dims() int {
	// Most Ollama embedding models return 768 or 1024 dimensions
	// For safety, we'll return 768 as the default and trust stored embeddings
	// to validate on retrieval.
	return 768
}

// OpenAIEmbedder generates embeddings using OpenAI's embedding API.
// See: https://platform.openai.com/docs/guides/embeddings
type OpenAIEmbedder struct {
	apiKey string // OpenAI API key
	model  string // e.g., "text-embedding-3-small"
	client *http.Client
}

// NewOpenAIEmbedder creates an embedder using OpenAI's API.
// Default model is "text-embedding-3-small" (1536 dimensions).
func NewOpenAIEmbedder(apiKey string) *OpenAIEmbedder {
	return &OpenAIEmbedder{
		apiKey: apiKey,
		model:  "text-embedding-3-small",
		client: &http.Client{},
	}
}

// NewOpenAIEmbedderWithModel creates an embedder using a specific OpenAI model.
func NewOpenAIEmbedderWithModel(apiKey, model string) *OpenAIEmbedder {
	return &OpenAIEmbedder{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{},
	}
}

// Embed generates a vector embedding for the given text using OpenAI.
func (e *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	type request struct {
		Model string `json:"model"`
		Input string `json:"input"`
	}
	type embedding struct {
		Embedding []float64 `json:"embedding"`
	}
	type response struct {
		Data []embedding `json:"data"`
	}

	payload := request{Model: e.model, Input: text}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("OpenAI: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("OpenAI: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OpenAI: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OpenAI: status %d: %s", resp.StatusCode, string(respBody))
	}

	var result response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("OpenAI: decode response: %w", err)
	}

	if len(result.Data) == 0 {
		return nil, fmt.Errorf("OpenAI: no embeddings in response")
	}

	// Convert float64 to float32
	vec := make([]float32, len(result.Data[0].Embedding))
	for i, v := range result.Data[0].Embedding {
		vec[i] = float32(v)
	}

	return vec, nil
}

// Dims returns the dimensionality of text-embedding-3-small (1536).
func (e *OpenAIEmbedder) Dims() int {
	// text-embedding-3-small: 1536
	// text-embedding-3-large: 3072
	// For simplicity, return 1536 as default and validate on retrieval.
	return 1536
}

// LLMCompleter is an interface for LLM-based text generation.
// Used by ingestors to extract entities from raw text.
type LLMCompleter interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

// ErrEmbedderUnavailable indicates the embedder service is not reachable.
var ErrEmbedderUnavailable = fmt.Errorf("embedder service unavailable")

// HealthCheck verifies that the embedder is reachable and responsive.
func (e *OllamaEmbedder) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", e.baseURL+"/api/tags", nil)
	if err != nil {
		return err
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return ErrEmbedderUnavailable
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ErrEmbedderUnavailable
	}

	return nil
}

// HealthCheck verifies that the OpenAI API key is valid.
func (e *OpenAIEmbedder) HealthCheck(ctx context.Context) error {
	// Quick validation by attempting a minimal embedding
	_, err := e.Embed(ctx, "test")
	if err != nil {
		return ErrEmbedderUnavailable
	}
	return nil
}
