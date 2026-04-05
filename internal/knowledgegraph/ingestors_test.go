package knowledgegraph

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	kg "github.com/julianshen/rubichan/pkg/knowledgegraph"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// mockCompleter returns fixed responses for testing
type mockCompleter struct {
	response string
}

func (m *mockCompleter) Complete(ctx context.Context, prompt string) (string, error) {
	return m.response, nil
}

func TestLLMIngestor(t *testing.T) {
	tmpDir := t.TempDir()
	g, err := openGraph(context.Background(), tmpDir, []kg.Option{})
	require.NoError(t, err)
	defer g.Close()

	// LLM response with entities
	response := `[
  {
    "id": "adr-go",
    "kind": "architecture",
    "title": "Go Language",
    "tags": ["language"],
    "body": "We chose Go for performance.",
    "relationships": []
  }
]`

	completer := &mockCompleter{response: response}
	ingestor := NewLLMIngestor(completer)

	count, err := ingestor.Ingest(context.Background(), g.(*KnowledgeGraph), "test text", kg.SourceLLM)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	// Verify entity was stored
	e, err := g.Get(context.Background(), "adr-go")
	require.NoError(t, err)
	require.NotNil(t, e)
	require.Equal(t, "Go Language", e.Title)
	require.Equal(t, kg.SourceLLM, e.Source)
}

func TestLLMIngestorEmptyResponse(t *testing.T) {
	tmpDir := t.TempDir()
	g, err := openGraph(context.Background(), tmpDir, []kg.Option{})
	require.NoError(t, err)
	defer g.Close()

	completer := &mockCompleter{response: "[]"}
	ingestor := NewLLMIngestor(completer)

	count, err := ingestor.Ingest(context.Background(), g.(*KnowledgeGraph), "test", kg.SourceLLM)
	require.NoError(t, err)
	require.Equal(t, 0, count)
}

func TestLLMIngestorEmptyText(t *testing.T) {
	tmpDir := t.TempDir()
	g, err := openGraph(context.Background(), tmpDir, []kg.Option{})
	require.NoError(t, err)
	defer g.Close()

	completer := &mockCompleter{}
	ingestor := NewLLMIngestor(completer)

	count, err := ingestor.Ingest(context.Background(), g.(*KnowledgeGraph), "", kg.SourceLLM)
	require.NoError(t, err)
	require.Equal(t, 0, count)
}

func TestManualIngestor(t *testing.T) {
	tmpDir := t.TempDir()
	g, err := openGraph(context.Background(), tmpDir, []kg.Option{})
	require.NoError(t, err)
	defer g.Close()

	// Create a markdown file with frontmatter
	mdFile := filepath.Join(tmpDir, "test.md")
	content := `---
id: test-entity
kind: architecture
title: Test Entity
source: manual
---
# Body content here
This is the body.`

	err = os.WriteFile(mdFile, []byte(content), 0o644)
	require.NoError(t, err)

	ingestor := NewManualIngestor()
	count, err := ingestor.Ingest(context.Background(), g.(*KnowledgeGraph), mdFile)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	// Verify entity was stored
	e, err := g.Get(context.Background(), "test-entity")
	require.NoError(t, err)
	require.NotNil(t, e)
	require.Equal(t, "Test Entity", e.Title)
	require.Equal(t, kg.SourceManual, e.Source)
}

func TestManualIngestorMissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	g, err := openGraph(context.Background(), tmpDir, []kg.Option{})
	require.NoError(t, err)
	defer g.Close()

	ingestor := NewManualIngestor()
	_, err = ingestor.Ingest(context.Background(), g.(*KnowledgeGraph), "/nonexistent/file.md")
	require.Error(t, err)
}

func TestFileIngestor(t *testing.T) {
	tmpDir := t.TempDir()
	g, err := openGraph(context.Background(), tmpDir, []kg.Option{})
	require.NoError(t, err)
	defer g.Close()

	// Create a file to analyze
	file := filepath.Join(tmpDir, "code.go")
	err = os.WriteFile(file, []byte("package main\nfunc main() { ... }"), 0o644)
	require.NoError(t, err)

	response := `[
  {
    "id": "module-main",
    "kind": "module",
    "title": "Main Module",
    "tags": ["go"],
    "body": "The main entry point.",
    "relationships": []
  }
]`

	completer := &mockCompleter{response: response}
	ingestor := NewFileIngestor(completer)

	count, err := ingestor.Ingest(context.Background(), g.(*KnowledgeGraph), file)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	e, err := g.Get(context.Background(), "module-main")
	require.NoError(t, err)
	require.NotNil(t, e)
	require.Equal(t, kg.SourceFile, e.Source)
}

func TestParseYAMLLine(t *testing.T) {
	tests := []struct {
		line    string
		wantKey string
		wantVal string
		wantOk  bool
	}{
		{"id: test-entity", "id", "test-entity", true},
		{"kind: architecture", "kind", "architecture", true},
		{"  title: My Title  ", "title", "My Title", true},
		{"no-colon", "", "", false},
	}

	for _, tt := range tests {
		key, val, ok := parseYAMLLine(tt.line)
		require.Equal(t, tt.wantOk, ok, "line: %s", tt.line)
		if ok {
			require.Equal(t, tt.wantKey, key)
			require.Equal(t, tt.wantVal, val)
		}
	}
}

func TestLLMIngestorParsesLayer(t *testing.T) {
	tmpDir := t.TempDir()
	g, err := openGraph(context.Background(), tmpDir, []kg.Option{})
	require.NoError(t, err)
	defer g.Close()

	// LLM response with layer field
	response := `[
  {
    "id": "team-001",
    "kind": "decision",
    "layer": "team",
    "title": "Team Decision",
    "tags": ["team"],
    "body": "A team-specific decision.",
    "relationships": []
  }
]`

	completer := &mockCompleter{response: response}
	ingestor := NewLLMIngestor(completer)

	count, err := ingestor.Ingest(context.Background(), g.(*KnowledgeGraph), "test text", kg.SourceLLM)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	// Verify entity has correct layer
	e, err := g.Get(context.Background(), "team-001")
	require.NoError(t, err)
	require.Equal(t, kg.EntityLayerTeam, e.Layer)
}

func TestManualIngestorParsesLayerFrontmatter(t *testing.T) {
	tmpDir := t.TempDir()
	g, err := openGraph(context.Background(), tmpDir, []kg.Option{})
	require.NoError(t, err)
	defer g.Close()

	// Create a file with layer in frontmatter
	filePath := filepath.Join(tmpDir, "session-entity.md")
	content := `---
id: session-001
kind: gotcha
layer: session
title: Session Gotcha
---
This is a session-specific gotcha.`

	err = os.WriteFile(filePath, []byte(content), 0o644)
	require.NoError(t, err)

	ingestor := NewManualIngestor()
	count, err := ingestor.Ingest(context.Background(), g.(*KnowledgeGraph), filePath)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	// Verify entity has correct layer
	e, err := g.Get(context.Background(), "session-001")
	require.NoError(t, err)
	require.Equal(t, kg.EntityLayerSession, e.Layer)
}

func TestReadEntityFromBytes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantID   string
		wantKind string
		wantErr  bool
	}{
		{
			name: "valid entity",
			input: `---
id: test-1
kind: decision
title: Test
---
Body content`,
			wantID:   "test-1",
			wantKind: "decision",
		},
		{
			name:    "no frontmatter",
			input:   "Just plain text",
			wantErr: false,
		},
		{
			name: "missing id",
			input: `---
kind: decision
---
Body`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := readEntityFromBytes([]byte(tt.input))
			if tt.wantErr {
				require.Error(t, err)
			} else {
				if tt.input == "Just plain text" {
					require.Nil(t, e)
				} else {
					require.NoError(t, err)
					require.Equal(t, tt.wantID, e.ID)
					require.Equal(t, kg.EntityKind(tt.wantKind), e.Kind)
				}
			}
		})
	}
}
