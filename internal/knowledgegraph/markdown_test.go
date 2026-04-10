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

func TestValidKindsCoversAllEntityKinds(t *testing.T) {
	// Catch drift: if a new EntityKind constant is added to pkg/knowledgegraph
	// but not to validKinds, this test fails.
	allKinds := []kg.EntityKind{
		kg.KindArchitecture,
		kg.KindDecision,
		kg.KindGotcha,
		kg.KindPattern,
		kg.KindModule,
		kg.KindIntegration,
	}
	for _, k := range allKinds {
		require.True(t, validKinds[string(k)], "validKinds missing %q", k)
	}
	require.Len(t, validKinds, len(allKinds), "validKinds has entries not in EntityKind constants")
}

func TestValidateFrontmatterMissingID(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "no-id.md")
	content := "---\nkind: gotcha\ntitle: No ID\n---\nBody text."
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	_, err := readEntityFile(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing required field 'id'")
}

func TestValidateFrontmatterMissingKind(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "no-kind.md")
	content := "---\nid: test-001\ntitle: No Kind\n---\nBody text."
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	_, err := readEntityFile(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing required field 'kind'")
}

func TestValidateFrontmatterInvalidKind(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "bad-kind.md")
	content := "---\nid: test-001\nkind: foobar\ntitle: Bad Kind\n---\nBody."
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	_, err := readEntityFile(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid kind")
}

func TestValidateFrontmatterBadConfidence(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "bad-conf.md")
	content := "---\nid: test-001\nkind: gotcha\nconfidence: 1.5\n---\nBody."
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	_, err := readEntityFile(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "confidence must be between")
}

func TestWalkKnowledgeDirSkipsInvalidFiles(t *testing.T) {
	dir := tempDir(t)

	// Write a valid entity
	valid := &kg.Entity{
		ID: "valid-001", Kind: kg.KindArchitecture, Title: "Valid",
		Source: kg.SourceManual, Created: time.Now(), Updated: time.Now(),
	}
	require.NoError(t, writeEntityFile(dir, valid))

	// Write an invalid file (missing ID) directly
	badDir := filepath.Join(dir, "gotcha")
	require.NoError(t, os.MkdirAll(badDir, 0o755))
	badPath := filepath.Join(badDir, "invalid.md")
	require.NoError(t, os.WriteFile(badPath, []byte("---\nkind: gotcha\n---\nNo ID here."), 0o644))

	// Walk should skip the invalid file and return the valid one
	entities, err := walkKnowledgeDir(dir)
	require.NoError(t, err)
	require.Len(t, entities, 1)
	require.Equal(t, "valid-001", entities[0].ID)
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

func TestEntityToPathBaseLayer(t *testing.T) {
	e := &kg.Entity{
		ID:    "adr-001",
		Kind:  kg.KindArchitecture,
		Layer: kg.EntityLayerBase,
	}

	path := entityToPath("/knowledge", e)
	expected := filepath.Join("/knowledge", "architecture", "adr-001.md")
	require.Equal(t, expected, path)
}

func TestEntityToPathEmptyLayerIsSameAsBase(t *testing.T) {
	e := &kg.Entity{
		ID:    "adr-001",
		Kind:  kg.KindArchitecture,
		Layer: kg.EntityLayer(""), // empty should behave like base
	}

	path := entityToPath("/knowledge", e)
	expected := filepath.Join("/knowledge", "architecture", "adr-001.md")
	require.Equal(t, expected, path)
}

func TestEntityToPathTeamLayer(t *testing.T) {
	e := &kg.Entity{
		ID:    "adr-002",
		Kind:  kg.KindDecision,
		Layer: kg.EntityLayerTeam,
	}

	path := entityToPath("/knowledge", e)
	expected := filepath.Join("/knowledge", "team", "decision", "adr-002.md")
	require.Equal(t, expected, path)
}

func TestEntityToPathSessionLayer(t *testing.T) {
	e := &kg.Entity{
		ID:    "adr-003",
		Kind:  kg.KindPattern,
		Layer: kg.EntityLayerSession,
	}

	path := entityToPath("/knowledge", e)
	expected := filepath.Join("/knowledge", "session", "pattern", "adr-003.md")
	require.Equal(t, expected, path)
}

func TestWriteReadEntityTeamLayer(t *testing.T) {
	dir := tempDir(t)
	now := time.Now().Round(0)

	e := &kg.Entity{
		ID:      "team-001",
		Kind:    kg.KindGotcha,
		Title:   "Team Gotcha",
		Layer:   kg.EntityLayerTeam,
		Tags:    []string{"team", "convention"},
		Body:    "This is a team-specific gotcha.",
		Source:  kg.SourceManual,
		Created: now,
		Updated: now,
	}

	// Write entity
	err := writeEntityFile(dir, e)
	require.NoError(t, err)

	// Verify file exists at team-prefixed path
	expectedPath := filepath.Join(dir, "team", "gotcha", "team-001.md")
	_, err = os.Stat(expectedPath)
	require.NoError(t, err)

	// Read it back
	read, err := readEntityFile(expectedPath)
	require.NoError(t, err)

	// Verify all fields including Layer
	require.Equal(t, e.ID, read.ID)
	require.Equal(t, e.Kind, read.Kind)
	require.Equal(t, e.Title, read.Title)
	require.Equal(t, kg.EntityLayerTeam, read.Layer)
	require.Equal(t, e.Tags, read.Tags)
	require.Equal(t, e.Body, read.Body)
}

func TestWriteReadEntitySessionLayer(t *testing.T) {
	dir := tempDir(t)
	now := time.Now().Round(0)

	e := &kg.Entity{
		ID:      "session-001",
		Kind:    kg.KindModule,
		Title:   "Session-Specific Module",
		Layer:   kg.EntityLayerSession,
		Body:    "Ephemeral session finding.",
		Source:  kg.SourceLLM,
		Created: now,
		Updated: now,
	}

	// Write entity
	err := writeEntityFile(dir, e)
	require.NoError(t, err)

	// Verify file exists at session-prefixed path
	expectedPath := filepath.Join(dir, "session", "module", "session-001.md")
	_, err = os.Stat(expectedPath)
	require.NoError(t, err)

	// Read it back
	read, err := readEntityFile(expectedPath)
	require.NoError(t, err)

	// Verify Layer field
	require.Equal(t, kg.EntityLayerSession, read.Layer)
}

func TestWalkKnowledgeDirFindsAllLayers(t *testing.T) {
	dir := tempDir(t)

	// Create entities in all three layers
	baseEntity := &kg.Entity{
		ID:      "base-001",
		Kind:    kg.KindArchitecture,
		Title:   "Base Architecture",
		Layer:   kg.EntityLayerBase,
		Source:  kg.SourceManual,
		Created: time.Now(),
		Updated: time.Now(),
	}

	teamEntity := &kg.Entity{
		ID:      "team-001",
		Kind:    kg.KindDecision,
		Title:   "Team Decision",
		Layer:   kg.EntityLayerTeam,
		Source:  kg.SourceManual,
		Created: time.Now(),
		Updated: time.Now(),
	}

	sessionEntity := &kg.Entity{
		ID:      "session-001",
		Kind:    kg.KindGotcha,
		Title:   "Session Gotcha",
		Layer:   kg.EntityLayerSession,
		Source:  kg.SourceLLM,
		Created: time.Now(),
		Updated: time.Now(),
	}

	for _, e := range []*kg.Entity{baseEntity, teamEntity, sessionEntity} {
		err := writeEntityFile(dir, e)
		require.NoError(t, err)
	}

	// Walk directory (should recursively find all layers)
	read, err := walkKnowledgeDir(dir)
	require.NoError(t, err)

	// Should find all three entities
	require.Len(t, read, 3)

	// Verify all IDs and layers are present
	found := map[string]kg.EntityLayer{}
	for _, e := range read {
		found[e.ID] = e.Layer
	}

	require.Equal(t, kg.EntityLayerBase, found["base-001"])
	require.Equal(t, kg.EntityLayerTeam, found["team-001"])
	require.Equal(t, kg.EntityLayerSession, found["session-001"])
}

func TestWalkKnowledgeDirSkipsEmptyIDs(t *testing.T) {
	dir := tempDir(t)
	kindDir := filepath.Join(dir, "module")
	require.NoError(t, os.MkdirAll(kindDir, 0o755))

	// Write a valid entity
	validContent := `---
id: valid-001
kind: module
title: Valid Module
source: manual
---
Valid body.`
	require.NoError(t, os.WriteFile(filepath.Join(kindDir, "valid.md"), []byte(validContent), 0o644))

	// Write an entity with empty id field
	emptyIDContent := `---
id: ""
kind: module
title: No ID Module
source: manual
---
Empty ID body.`
	require.NoError(t, os.WriteFile(filepath.Join(kindDir, "empty-id.md"), []byte(emptyIDContent), 0o644))

	// Write an entity with missing id field entirely
	missingIDContent := `---
kind: module
title: Missing ID Module
source: manual
---
Missing ID body.`
	require.NoError(t, os.WriteFile(filepath.Join(kindDir, "missing-id.md"), []byte(missingIDContent), 0o644))

	// Walk should only return the valid entity
	read, err := walkKnowledgeDir(dir)
	require.NoError(t, err)
	require.Len(t, read, 1, "should skip entities with empty IDs")
	require.Equal(t, "valid-001", read[0].ID)
}
