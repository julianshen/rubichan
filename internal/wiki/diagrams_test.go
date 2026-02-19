package wiki

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
