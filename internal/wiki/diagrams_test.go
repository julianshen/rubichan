package wiki

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/text"
)

func TestDefaultDiagramConfig(t *testing.T) {
	cfg := DefaultDiagramConfig()
	assert.Equal(t, "mermaid", cfg.Format)
}

func TestGenerateArchitectureDiagram(t *testing.T) {
	files := []ScannedFile{
		{Path: "handler.go", Language: "go", Module: "internal/handler"},
	}
	analysis := &AnalysisResult{
		Modules: []ModuleAnalysis{
			{Module: "internal/handler", Summary: "Handles HTTP requests and routing"},
			{Module: "internal/store", Summary: "Persistence layer for data storage"},
		},
		Architecture: "Layered architecture with handler and store.",
	}

	diagrams, err := GenerateDiagrams(context.Background(), files, analysis, nil, DefaultDiagramConfig())
	require.NoError(t, err)

	// Find the architecture diagram.
	var archDiagram *Diagram
	for i := range diagrams {
		if diagrams[i].Type == "architecture" {
			archDiagram = &diagrams[i]
			break
		}
	}
	require.NotNil(t, archDiagram, "architecture diagram should exist")
	assert.Equal(t, "Architecture Overview", archDiagram.Title)
	assert.NotEmpty(t, archDiagram.Content)
	assert.Contains(t, archDiagram.Content, "graph TD")
}

func TestGenerateDependencyDiagram(t *testing.T) {
	files := []ScannedFile{
		{Path: "handler.go", Language: "go", Module: "internal/handler", Imports: []string{"github.com/example/internal/store"}},
		{Path: "store.go", Language: "go", Module: "internal/store", Imports: []string{}},
	}
	analysis := &AnalysisResult{
		Modules: []ModuleAnalysis{
			{Module: "internal/handler", Summary: "HTTP handler"},
			{Module: "internal/store", Summary: "Data store"},
		},
	}

	diagrams, err := GenerateDiagrams(context.Background(), files, analysis, nil, DefaultDiagramConfig())
	require.NoError(t, err)

	var depDiagram *Diagram
	for i := range diagrams {
		if diagrams[i].Type == "dependency" {
			depDiagram = &diagrams[i]
			break
		}
	}
	require.NotNil(t, depDiagram, "dependency diagram should exist")
	assert.Equal(t, "Module Dependencies", depDiagram.Title)
	assert.Contains(t, depDiagram.Content, "graph LR")
}

func TestGenerateDiagramsUnsupportedFormat(t *testing.T) {
	cfg := DiagramConfig{Format: "d2"}
	_, err := GenerateDiagrams(context.Background(), nil, &AnalysisResult{}, nil, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported diagram format")
}

func TestGenerateDiagramsWithLLMSequence(t *testing.T) {
	files := []ScannedFile{
		{Path: "handler.go", Language: "go", Module: "internal/handler"},
	}
	analysis := &AnalysisResult{
		Modules: []ModuleAnalysis{
			{Module: "internal/handler", Summary: "Handles HTTP requests"},
		},
		Architecture: "A layered architecture with HTTP handlers.",
	}

	llm := &mockLLMCompleter{
		responses: map[string]string{
			"sequenceDiagram": "sequenceDiagram\n    Client->>Handler: HTTP Request\n    Handler->>Store: Query\n    Store-->>Handler: Result\n    Handler-->>Client: Response",
		},
	}

	diagrams, err := GenerateDiagrams(context.Background(), files, analysis, llm, DefaultDiagramConfig())
	require.NoError(t, err)

	var seqDiagram *Diagram
	for i := range diagrams {
		if diagrams[i].Type == "sequence" {
			seqDiagram = &diagrams[i]
			break
		}
	}
	require.NotNil(t, seqDiagram, "sequence diagram should exist when LLM is provided")
	assert.Equal(t, "Key Sequences", seqDiagram.Title)
	assert.Contains(t, seqDiagram.Content, "sequenceDiagram")

	// Verify the LLM was called with a prompt containing "sequenceDiagram".
	llm.mu.Lock()
	defer llm.mu.Unlock()
	found := false
	for _, call := range llm.calls {
		if strings.Contains(call, "sequenceDiagram") {
			found = true
			break
		}
	}
	assert.True(t, found, "LLM should have been called with a prompt mentioning sequenceDiagram")
}

func TestGenerateDiagramsCancellationReturnsContextErrorWithoutWarnings(t *testing.T) {
	files := []ScannedFile{
		{Path: "handler.go", Language: "go", Module: "internal/handler"},
	}
	analysis := &AnalysisResult{
		Modules: []ModuleAnalysis{
			{Module: "internal/handler", Summary: "Handles HTTP requests"},
		},
		Architecture: "A layered architecture with HTTP handlers.",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// This test redirects the package-level logger and must stay non-parallel.
	var logs bytes.Buffer
	origWriter := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(origWriter)

	diagrams, err := GenerateDiagrams(ctx, files, analysis, &cancelingLLMCompleter{}, DefaultDiagramConfig())
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, diagrams)
	assert.NotContains(t, logs.String(), "WARNING:")
}

func TestGenerateDiagramsEmptyLLMResponseForSequence(t *testing.T) {
	// When the LLM returns an empty response for sequence diagram generation,
	// the sequence diagram should be skipped with a warning, but other diagrams
	// should still be generated.
	files := []ScannedFile{
		{Path: "handler.go", Language: "go", Module: "internal/handler"},
	}
	analysis := &AnalysisResult{
		Modules: []ModuleAnalysis{
			{Module: "internal/handler", Summary: "Handles HTTP requests"},
			{Module: "internal/store", Summary: "Data store"},
		},
		Architecture: "A layered architecture with HTTP handlers.",
	}

	// Mock LLM that returns empty string for sequence diagram prompts.
	// It validates empty responses just like the real LLMCompleter.
	llm := &validatingLLMCompleter{
		responses: map[string]string{
			"sequenceDiagram": "", // Empty response
		},
	}

	var logs bytes.Buffer
	origWriter := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(origWriter)

	diagrams, err := GenerateDiagrams(context.Background(), files, analysis, llm, DefaultDiagramConfig())
	require.NoError(t, err)

	// Should have generated diagrams (architecture, dependency, dataflow) but NOT sequence
	assert.Greater(t, len(diagrams), 0)
	for _, diagram := range diagrams {
		assert.NotEqual(t, "sequence", diagram.Type, "sequence diagram should be skipped for empty response")
	}

	// Should have logged a warning about sequence diagram failure
	logOutput := logs.String()
	assert.Contains(t, logOutput, "WARNING:")
	assert.Contains(t, logOutput, "sequence diagram generation failed")
	assert.Contains(t, logOutput, "empty response")
}

func TestGenerateArchitectureDiagram_HasEdges(t *testing.T) {
	files := []ScannedFile{
		{Path: "handler.go", Language: "go", Module: "internal/handler", Imports: []string{"github.com/example/internal/store"}},
		{Path: "store.go", Language: "go", Module: "internal/store", Imports: []string{}},
	}
	modules := []ModuleAnalysis{
		{Module: "internal/handler", Summary: "HTTP handler"},
		{Module: "internal/store", Summary: "Data store"},
	}

	diagram := generateArchitectureDiagram(files, modules)

	assert.Equal(t, "Architecture Overview", diagram.Title)
	assert.Equal(t, "architecture", diagram.Type)
	assert.Contains(t, diagram.Content, "graph TD")
	// Should contain nodes.
	assert.Contains(t, diagram.Content, "internal_handler")
	assert.Contains(t, diagram.Content, "internal_store")
	// Should contain edge from handler to store.
	assert.Contains(t, diagram.Content, "internal_handler --> internal_store")
}

func TestGenerateArchitectureDiagram_NilFiles(t *testing.T) {
	modules := []ModuleAnalysis{
		{Module: "internal/handler", Summary: "HTTP handler"},
	}

	diagram := generateArchitectureDiagram(nil, modules)

	assert.Equal(t, "Architecture Overview", diagram.Title)
	assert.Contains(t, diagram.Content, "graph TD")
	assert.Contains(t, diagram.Content, "internal_handler")
	assert.NotContains(t, diagram.Content, "-->")
}

func TestFirstSentence(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "single sentence", in: "Handles HTTP requests.", want: "Handles HTTP requests."},
		{name: "multiple sentences", in: "Handles HTTP requests. Also does routing.", want: "Handles HTTP requests."},
		{name: "exclamation", in: "Important module! Do not remove.", want: "Important module!"},
		{name: "question", in: "Is this needed? Probably.", want: "Is this needed?"},
		{name: "no terminator", in: "No sentence ending here", want: "No sentence ending here"},
		{name: "empty", in: "", want: ""},
		{name: "just period", in: ".", want: "."},
		{name: "abbreviation e.g.", in: "Supports e.g. JSON and YAML.", want: "Supports e.g."},
		{name: "decimal v2.0", in: "Uses v2.0 of the API. Next version coming.", want: "Uses v2.0 of the API."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, firstSentence(tt.in))
		})
	}
}

func TestArchitectureDiagramShortLabels(t *testing.T) {
	modules := []ModuleAnalysis{
		{
			Module:  "internal/handler",
			Summary: "The provided text describes a module structure for handling HTTP requests and routing them to appropriate handlers.",
		},
		{
			Module:  "internal/store",
			Summary: "Persistence layer for data storage.",
		},
		{
			Module:  "internal/empty",
			Summary: "",
		},
	}

	diagram := generateArchitectureDiagram(nil, modules)

	// Long first sentence should be capped at 60 runes.
	// First sentence: "The provided text describes a module structure for handling HTTP requests and routing them to appropriate handlers."
	// After truncateUTF8(_, 60): "The provided text describes a module structure for handling "
	assert.Contains(t, diagram.Content, "The provided text describes a module structure for handling ")
	assert.NotContains(t, diagram.Content, "appropriate handlers.")
	// Short summary should use first sentence as-is.
	assert.Contains(t, diagram.Content, "Persistence layer for data storage.")
	// Empty summary module should have no \\n separator.
	assert.Contains(t, diagram.Content, `internal_empty["internal/empty"]`)
	// Should NOT contain truncated mid-word text.
	assert.NotContains(t, diagram.Content, "str...")
}

// validatingLLMCompleter is a mock LLM that validates empty responses,
// matching the behavior of the real LLMCompleter.Complete().
type validatingLLMCompleter struct {
	responses map[string]string
}

func (m *validatingLLMCompleter) Complete(ctx context.Context, prompt string) (string, error) {
	for key, resp := range m.responses {
		if strings.Contains(prompt, key) {
			// Mimic the real LLMCompleter behavior: return error for empty/whitespace responses
			if text.IsEmptyResponse(resp) {
				return "", fmt.Errorf("empty response from model")
			}
			return resp, nil
		}
	}
	return "default response", nil
}
