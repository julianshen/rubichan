package knowledgegraph

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	kg "github.com/julianshen/rubichan/pkg/knowledgegraph"
	"github.com/stretchr/testify/require"
)

func tempDir(t *testing.T) string {
	dir, err := os.MkdirTemp("", "kg-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestEntityToPath(t *testing.T) {
	e := &kg.Entity{
		ID:   "adr-001",
		Kind: kg.KindArchitecture,
	}

	path := entityToPath("/knowledge", e)
	expected := filepath.Join("/knowledge", "architecture", "adr-001.md")
	require.Equal(t, expected, path)
}

func TestWriteReadEntity(t *testing.T) {
	dir := tempDir(t)
	now := time.Now().Round(0)

	e := &kg.Entity{
		ID:      "test-001",
		Kind:    kg.KindGotcha,
		Title:   "Test Gotcha",
		Tags:    []string{"sqlite", "concurrency"},
		Body:    "This is the body content.\nWith multiple lines.",
		Source:  kg.SourceManual,
		Created: now,
		Updated: now,
		Relationships: []kg.Relationship{
			{Kind: kg.RelJustifies, Target: "test-002"},
			{Kind: kg.RelRelatesTo, Target: "test-003"},
		},
	}

	// Write entity
	err := writeEntityFile(dir, e)
	require.NoError(t, err)

	// Verify file exists
	expectedPath := filepath.Join(dir, "gotcha", "test-001.md")
	_, err = os.Stat(expectedPath)
	require.NoError(t, err)

	// Read it back
	read, err := readEntityFile(expectedPath)
	require.NoError(t, err)

	// Verify all fields
	require.Equal(t, e.ID, read.ID)
	require.Equal(t, e.Kind, read.Kind)
	require.Equal(t, e.Title, read.Title)
	require.Equal(t, e.Tags, read.Tags)
	require.Equal(t, e.Body, read.Body)
	require.Equal(t, e.Source, read.Source)
	require.Equal(t, e.Created.Unix(), read.Created.Unix()) // time.Time precision
	require.Equal(t, e.Updated.Unix(), read.Updated.Unix())
	require.Len(t, read.Relationships, 2)
	require.Equal(t, kg.RelJustifies, read.Relationships[0].Kind)
	require.Equal(t, "test-002", read.Relationships[0].Target)
}

func TestWriteReadEntityWithLifecycleFields(t *testing.T) {
	dir := tempDir(t)
	now := time.Now().Round(0)

	e := &kg.Entity{
		ID:         "lifecycle-001",
		Kind:       kg.KindArchitecture,
		Title:      "Entity with Lifecycle",
		Tags:       []string{"versioned"},
		Body:       "Body content.",
		Source:     kg.SourceManual,
		Created:    now,
		Updated:    now,
		Version:    "1.0.0",
		Confidence: 0.95,
		// UsageCount and LastUsed are runtime-only, not persisted in markdown
	}

	// Write entity
	err := writeEntityFile(dir, e)
	require.NoError(t, err)

	// Read it back
	expectedPath := filepath.Join(dir, "architecture", "lifecycle-001.md")
	read, err := readEntityFile(expectedPath)
	require.NoError(t, err)

	// Verify lifecycle fields (Version and Confidence go to markdown)
	require.Equal(t, e.ID, read.ID)
	require.Equal(t, e.Version, read.Version)
	require.Equal(t, e.Confidence, read.Confidence)

	// UsageCount and LastUsed should be zero/nil from markdown read
	require.Equal(t, 0, read.UsageCount)
	require.True(t, read.LastUsed.IsZero())
}

func TestWriteReadEntityMinimal(t *testing.T) {
	dir := tempDir(t)

	e := &kg.Entity{
		ID:      "minimal",
		Kind:    kg.KindArchitecture,
		Title:   "Minimal Entity",
		Source:  kg.SourceLLM,
		Created: time.Now(),
		Updated: time.Now(),
	}

	err := writeEntityFile(dir, e)
	require.NoError(t, err)

	read, err := readEntityFile(filepath.Join(dir, "architecture", "minimal.md"))
	require.NoError(t, err)

	require.Equal(t, e.ID, read.ID)
	require.Equal(t, e.Kind, read.Kind)
	require.Equal(t, e.Title, read.Title)
	require.Empty(t, read.Tags)
	require.Empty(t, read.Body)
	require.Empty(t, read.Relationships)
}

func TestWalkKnowledgeDir(t *testing.T) {
	dir := tempDir(t)

	// Create three entities
	entities := []*kg.Entity{
		{ID: "arch-001", Kind: kg.KindArchitecture, Title: "Arch 1", Source: kg.SourceManual, Created: time.Now(), Updated: time.Now()},
		{ID: "gotcha-001", Kind: kg.KindGotcha, Title: "Gotcha 1", Source: kg.SourceGit, Created: time.Now(), Updated: time.Now()},
		{ID: "pattern-001", Kind: kg.KindPattern, Title: "Pattern 1", Source: kg.SourceLLM, Created: time.Now(), Updated: time.Now()},
	}

	for _, e := range entities {
		err := writeEntityFile(dir, e)
		require.NoError(t, err)
	}

	// Walk directory
	read, err := walkKnowledgeDir(dir)
	require.NoError(t, err)

	// Should find all three entities
	require.Len(t, read, 3)

	// Verify IDs are present
	ids := map[string]bool{}
	for _, e := range read {
		ids[e.ID] = true
	}
	require.True(t, ids["arch-001"])
	require.True(t, ids["gotcha-001"])
	require.True(t, ids["pattern-001"])
}

func TestWalkKnowledgeDirNonexistent(t *testing.T) {
	// Non-existent directory should return empty list, not error
	read, err := walkKnowledgeDir("/nonexistent/path")
	require.NoError(t, err)
	require.Empty(t, read)
}

func TestWalkKnowledgeDirEmpty(t *testing.T) {
	dir := tempDir(t)

	// Empty directory should return empty list
	read, err := walkKnowledgeDir(dir)
	require.NoError(t, err)
	require.Empty(t, read)
}

func TestReadEntityMissingFrontmatter(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "bad.md")

	// Write a file without proper frontmatter
	err := os.WriteFile(path, []byte("No frontmatter here"), 0o644)
	require.NoError(t, err)

	_, err = readEntityFile(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing frontmatter delimiters")
}

func TestEntityWithSpecialCharacters(t *testing.T) {
	dir := tempDir(t)

	e := &kg.Entity{
		ID:      "special-001",
		Kind:    kg.KindDecision,
		Title:   "Decision with special chars: @#$%",
		Body:    "Line 1\nLine 2 with 中文\nLine 3 with emoji 🎉",
		Source:  kg.SourceManual,
		Created: time.Now(),
		Updated: time.Now(),
	}

	err := writeEntityFile(dir, e)
	require.NoError(t, err)

	read, err := readEntityFile(filepath.Join(dir, "decision", "special-001.md"))
	require.NoError(t, err)

	require.Equal(t, e.Title, read.Title)
	require.Equal(t, e.Body, read.Body)
}
