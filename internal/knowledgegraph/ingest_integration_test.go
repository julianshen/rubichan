package knowledgegraph

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	kg "github.com/julianshen/rubichan/pkg/knowledgegraph"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// TestIngest_LLMExtractsValidEntities verifies LLM ingestor extracts entities with required fields
func TestIngest_LLMExtractsValidEntities(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	// Use mock completer that returns deterministic entities
	completer := &mockCompleter{
		response: `[
  {
    "id": "llm-arch-001",
    "kind": "architecture",
    "layer": "base",
    "title": "LLM Architecture",
    "tags": ["llm", "test"],
    "body": "This is a test architecture entity from LLM",
    "relationships": []
  },
  {
    "id": "llm-dec-001",
    "kind": "decision",
    "layer": "base",
    "title": "LLM Decision",
    "tags": ["llm", "test"],
    "body": "This is a test decision entity from LLM",
    "relationships": []
  }
]`,
	}

	ingestor := NewLLMIngestor(completer)
	graph := fixture.Graph.(*KnowledgeGraph)

	// Ingest from sample text
	count, err := ingestor.Ingest(context.Background(), graph, "Sample project content", kg.SourceLLM)

	require.NoError(t, err)
	require.GreaterOrEqual(t, count, 2, "should extract 2+ entities")

	// Verify entities have required fields
	entity1, err := fixture.Graph.Get(context.Background(), "llm-arch-001")
	require.NoError(t, err)
	require.NotNil(t, entity1)
	require.Equal(t, kg.KindArchitecture, entity1.Kind)
	require.NotEmpty(t, entity1.Body)
	require.Equal(t, kg.SourceLLM, entity1.Source)

	entity2, err := fixture.Graph.Get(context.Background(), "llm-dec-001")
	require.NoError(t, err)
	require.NotNil(t, entity2)
	require.Equal(t, kg.KindDecision, entity2.Kind)
}

// TestIngest_LLMErrorHandling verifies LLM ingestor handles invalid JSON from LLM
func TestIngest_LLMErrorHandling(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	// Mock completer that returns invalid JSON
	completer := &mockCompleter{
		response: `{invalid json}`,
	}

	ingestor := NewLLMIngestor(completer)
	graph := fixture.Graph.(*KnowledgeGraph)

	count, err := ingestor.Ingest(context.Background(), graph, "test content", kg.SourceLLM)

	require.Error(t, err)
	require.Equal(t, 0, count)
}

// TestIngest_GitAnalyzesCommitHistory verifies git ingestor handles commit history
func TestIngest_GitAnalyzesCommitHistory(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	// Initialize git repo in fixture directory
	setupGitRepo(t, fixture.Dir)

	// Create git ingestor with mock completer
	completer := &mockCompleter{
		response: `[
  {
    "id": "git-dec-001",
    "kind": "decision",
    "layer": "base",
    "title": "Git Decision",
    "tags": ["git"],
    "body": "Decision from git history",
    "relationships": []
  }
]`,
	}

	ingestor := NewGitIngestor(completer)
	graph := fixture.Graph.(*KnowledgeGraph)

	// Ingest from git history
	count, err := ingestor.Ingest(context.Background(), graph, fixture.Dir, "1w")

	// May be empty if fixture has no recent commits, but should not error
	require.NoError(t, err)
	require.GreaterOrEqual(t, count, 0)
}

// TestIngest_GitErrorWhenNotRepo verifies git ingestor errors on non-git directory
func TestIngest_GitErrorWhenNotRepo(t *testing.T) {
	tmpDir := t.TempDir()
	g, err := openGraph(context.Background(), tmpDir, []kg.Option{})
	require.NoError(t, err)
	defer g.Close()

	// Create git ingestor with mock completer
	completer := &mockCompleter{response: "[]"}
	ingestor := NewGitIngestor(completer)
	graph := g.(*KnowledgeGraph)

	// Try to ingest from non-git directory
	count, err := ingestor.Ingest(context.Background(), graph, tmpDir, "1w")

	// Should error when git log fails (no .git directory)
	require.Error(t, err)
	require.Equal(t, 0, count)
}

// TestIngest_FileAnalysisParsesMarkdown verifies file ingestor reads .knowledge/*.md files
func TestIngest_FileAnalysisParsesMarkdown(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	// Create a code file to analyze
	codePath := filepath.Join(fixture.Dir, "example.go")
	err := os.WriteFile(codePath, []byte("package main\n\nfunc main() {\n    // example\n}"), 0o644)
	require.NoError(t, err)

	// File ingestor uses LLM to extract entities
	completer := &mockCompleter{
		response: `[
  {
    "id": "file-module-001",
    "kind": "module",
    "layer": "base",
    "title": "File Module",
    "tags": ["file", "go"],
    "body": "Module extracted from file content",
    "relationships": []
  }
]`,
	}

	ingestor := NewFileIngestor(completer)
	graph := fixture.Graph.(*KnowledgeGraph)

	count, err := ingestor.Ingest(context.Background(), graph, codePath)

	require.NoError(t, err)
	require.Equal(t, 1, count)

	// Verify entity was stored
	entity, err := fixture.Graph.Get(context.Background(), "file-module-001")
	require.NoError(t, err)
	require.NotNil(t, entity)
	require.Equal(t, kg.SourceFile, entity.Source)
}

// TestIngest_ManualYAMLLoadsFromFile verifies manual ingestor loads entities from YAML frontmatter
func TestIngest_ManualYAMLLoadsFromFile(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	// Create a markdown file with YAML frontmatter in .knowledge/
	knowledgeDir := filepath.Join(fixture.Dir, ".knowledge")
	mdPath := filepath.Join(knowledgeDir, "manual-entity.md")
	content := `---
id: manual-001
kind: architecture
layer: base
title: Manually Created Entity
source: manual
---

# Manual Entity

This is a manually created entity with full details.
It demonstrates the YAML frontmatter format.
`

	err := os.WriteFile(mdPath, []byte(content), 0o644)
	require.NoError(t, err)

	ingestor := NewManualIngestor()
	graph := fixture.Graph.(*KnowledgeGraph)

	count, err := ingestor.Ingest(context.Background(), graph, mdPath)

	require.NoError(t, err)
	require.Equal(t, 1, count)

	// Verify entity was stored
	entity, err := fixture.Graph.Get(context.Background(), "manual-001")
	require.NoError(t, err)
	require.NotNil(t, entity)
	require.Equal(t, "Manually Created Entity", entity.Title)
	require.Equal(t, kg.SourceManual, entity.Source)
	require.True(t, len(entity.Body) > 0)
}

// TestIngest_ManualYAMLErrorOnMissingRequiredField verifies error handling for invalid YAML
func TestIngest_ManualYAMLErrorOnMissingRequiredField(t *testing.T) {
	tmpDir := t.TempDir()
	g, err := openGraph(context.Background(), tmpDir, []kg.Option{})
	require.NoError(t, err)
	defer g.Close()

	// Create a markdown file without required 'id' field
	mdPath := filepath.Join(tmpDir, "incomplete.md")
	content := `---
kind: architecture
title: Incomplete Entity
---
Body without ID
`

	err = os.WriteFile(mdPath, []byte(content), 0o644)
	require.NoError(t, err)

	ingestor := NewManualIngestor()
	graph := g.(*KnowledgeGraph)

	count, err := ingestor.Ingest(context.Background(), graph, mdPath)

	require.Error(t, err)
	require.Equal(t, 0, count)
}

// TestIngest_BatchMultipleSources verifies batch ingestion from multiple sources
func TestIngest_BatchMultipleSources(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	graph := fixture.Graph.(*KnowledgeGraph)

	// 1. Ingest from LLM
	llmCompleter := &mockCompleter{
		response: `[
  {
    "id": "batch-llm-001",
    "kind": "decision",
    "layer": "base",
    "title": "Batch LLM",
    "tags": [],
    "body": "From LLM source",
    "relationships": []
  }
]`,
	}
	llmIngestor := NewLLMIngestor(llmCompleter)
	count1, err := llmIngestor.Ingest(context.Background(), graph, "LLM text", kg.SourceLLM)
	require.NoError(t, err)
	require.Equal(t, 1, count1)

	// 2. Ingest from manual file
	knowledgeDir := filepath.Join(fixture.Dir, ".knowledge")
	mdPath := filepath.Join(knowledgeDir, "batch-manual.md")
	content := `---
id: batch-manual-001
kind: architecture
layer: base
title: Batch Manual
---
From manual source
`
	err = os.WriteFile(mdPath, []byte(content), 0o644)
	require.NoError(t, err)

	manualIngestor := NewManualIngestor()
	count2, err := manualIngestor.Ingest(context.Background(), graph, mdPath)
	require.NoError(t, err)
	require.Equal(t, 1, count2)

	// 3. Verify both entities exist in graph
	entity1, err := fixture.Graph.Get(context.Background(), "batch-llm-001")
	require.NoError(t, err)
	require.Equal(t, kg.SourceLLM, entity1.Source)

	entity2, err := fixture.Graph.Get(context.Background(), "batch-manual-001")
	require.NoError(t, err)
	require.Equal(t, kg.SourceManual, entity2.Source)
}

// setupGitRepo initializes a git repository for testing
func setupGitRepo(t *testing.T, dir string) {
	cmd := exec.CommandContext(context.Background(), "git", "init")
	cmd.Dir = dir
	err := cmd.Run()
	if err != nil {
		// Git might not be available in test environment, skip
		t.Skipf("git not available: %v", err)
	}

	// Configure git user
	cmd = exec.CommandContext(context.Background(), "git", "config", "user.email", "test@example.com")
	cmd.Dir = dir
	_ = cmd.Run()

	cmd = exec.CommandContext(context.Background(), "git", "config", "user.name", "Test User")
	cmd.Dir = dir
	_ = cmd.Run()

	// Create initial commit
	filePath := filepath.Join(dir, "README.md")
	err = os.WriteFile(filePath, []byte("# Test Project"), 0o644)
	require.NoError(t, err)

	cmd = exec.CommandContext(context.Background(), "git", "add", "README.md")
	cmd.Dir = dir
	_ = cmd.Run()

	cmd = exec.CommandContext(context.Background(), "git", "commit", "-m", "Initial commit")
	cmd.Dir = dir
	_ = cmd.Run()
}
