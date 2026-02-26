//go:build integration

package ollama

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testBaseURL = "http://localhost:11434"

func skipIfOllamaNotRunning(t *testing.T) {
	t.Helper()
	client := NewClient(testBaseURL)
	if !client.IsRunning(context.Background()) {
		t.Skip("Ollama is not running at " + testBaseURL)
	}
}

func TestIntegration_Version(t *testing.T) {
	skipIfOllamaNotRunning(t)
	client := NewClient(testBaseURL)
	ver, err := client.Version(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, ver)
}

func TestIntegration_ListModels(t *testing.T) {
	skipIfOllamaNotRunning(t)
	client := NewClient(testBaseURL)
	models, err := client.ListModels(context.Background())
	require.NoError(t, err)
	t.Logf("Found %d models", len(models))
	for _, m := range models {
		t.Logf("  %s (%d bytes)", m.Name, m.Size)
	}
}

func TestIntegration_StreamCompletion(t *testing.T) {
	skipIfOllamaNotRunning(t)
	client := NewClient(testBaseURL)
	models, err := client.ListModels(context.Background())
	require.NoError(t, err)
	if len(models) == 0 {
		t.Skip("No models available for streaming test")
	}

	p := New(testBaseURL)
	ch, err := p.Stream(context.Background(), provider.CompletionRequest{
		Model:     models[0].Name,
		Messages:  []provider.Message{{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "Say hello in exactly 3 words."}}}},
		MaxTokens: 20,
	})
	require.NoError(t, err)

	var gotText bool
	for evt := range ch {
		if evt.Type == "text_delta" {
			gotText = true
		}
	}
	assert.True(t, gotText, "should have received text events")
}
