package knowledgegraph

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
	kg "github.com/julianshen/rubichan/pkg/knowledgegraph"
)

func TestContextSelector(t *testing.T) {
	// Create a temporary knowledge directory
	tmpDir := t.TempDir()

	// Open a graph
	g, err := openGraph(context.Background(), tmpDir, []kg.Option{})
	require.NoError(t, err)
	defer g.Close()

	// Add some test entities
	entities := []*kg.Entity{
		{
			ID:      "arch-go",
			Kind:    kg.KindArchitecture,
			Title:   "Go Language Choice",
			Tags:    []string{"language", "runtime"},
			Body:    "Go was chosen for single-binary distribution and goroutine concurrency.",
			Source:  kg.SourceManual,
			Created: time.Now(),
			Updated: time.Now(),
		},
		{
			ID:      "decision-sql",
			Kind:    kg.KindDecision,
			Title:   "SQLite for Persistence",
			Tags:    []string{"database", "storage"},
			Body:    "We chose SQLite for its simplicity and no-dependency pure Go implementation.",
			Source:  kg.SourceManual,
			Created: time.Now(),
			Updated: time.Now(),
		},
		{
			ID:      "module-api",
			Kind:    kg.KindModule,
			Title:   "API Handler Module",
			Tags:    []string{"module", "http"},
			Body:    "Handles all HTTP requests and responses with proper error handling.",
			Source:  kg.SourceManual,
			Created: time.Now(),
			Updated: time.Now(),
		},
	}

	for _, e := range entities {
		err := g.Put(context.Background(), e)
		require.NoError(t, err)
	}

	// Create a selector
	selector := NewContextSelector(g.(*KnowledgeGraph))
	require.NotNil(t, selector)

	// Select with no budget constraint
	results, err := selector.Select(context.Background(), "database persistence", 0)
	require.NoError(t, err)
	require.Greater(t, len(results), 0)

	// Verify we got relevant results
	found := false
	for _, r := range results {
		if r.Entity.ID == "decision-sql" {
			found = true
			break
		}
	}
	require.True(t, found, "should find SQLite decision entity")
}

func TestContextSelectorWithBudget(t *testing.T) {
	tmpDir := t.TempDir()
	g, err := openGraph(context.Background(), tmpDir, []kg.Option{})
	require.NoError(t, err)
	defer g.Close()

	// Add multiple entities
	for i := 1; i <= 5; i++ {
		e := &kg.Entity{
			ID:      "entity-" + string(rune('0'+i)),
			Kind:    kg.KindArchitecture,
			Title:   "Entity " + string(rune('0'+i)),
			Tags:    []string{"test"},
			Body:    "This is a test entity with some content that will consume tokens.",
			Source:  kg.SourceManual,
			Created: time.Now(),
			Updated: time.Now(),
		}
		err := g.Put(context.Background(), e)
		require.NoError(t, err)
	}

	selector := NewContextSelector(g.(*KnowledgeGraph))

	// Select with small budget - should trim results
	results, err := selector.Select(context.Background(), "test", 100)
	require.NoError(t, err)
	// With budget of 100, we should get fewer results than without budget
	require.Less(t, len(results), 5)

	// Verify total tokens don't exceed budget
	totalTokens := 0
	for _, r := range results {
		totalTokens += r.EstimatedTokens
	}
	require.LessOrEqual(t, totalTokens, 100)
}

func TestContextSelectorEmptyGraph(t *testing.T) {
	tmpDir := t.TempDir()
	g, err := openGraph(context.Background(), tmpDir, []kg.Option{})
	require.NoError(t, err)
	defer g.Close()

	selector := NewContextSelector(g.(*KnowledgeGraph))
	results, err := selector.Select(context.Background(), "anything", 0)
	require.NoError(t, err)
	require.Len(t, results, 0)
}

func TestContextSelectorWithNullEmbedder(t *testing.T) {
	tmpDir := t.TempDir()
	// Explicitly use NullEmbedder (no vector search, FTS5 fallback)
	g, err := openGraph(context.Background(), tmpDir, []kg.Option{
		kg.WithEmbedder(kg.NullEmbedder{}),
	})
	require.NoError(t, err)
	defer g.Close()

	// Add test entity
	e := &kg.Entity{
		ID:      "test-entity",
		Kind:    kg.KindArchitecture,
		Title:   "Test Architecture",
		Tags:    []string{"test", "architecture"},
		Body:    "This is a test entity for FTS5 search.",
		Source:  kg.SourceManual,
		Created: time.Now(),
		Updated: time.Now(),
	}
	err = g.Put(context.Background(), e)
	require.NoError(t, err)

	selector := NewContextSelector(g.(*KnowledgeGraph))
	// Should work with FTS5 fallback even though no vector embedder
	results, err := selector.Select(context.Background(), "architecture", 0)
	require.NoError(t, err)
	require.Greater(t, len(results), 0)
}

func TestNewContextSelectorReturnsInterface(t *testing.T) {
	tmpDir := t.TempDir()
	g, err := openGraph(context.Background(), tmpDir, []kg.Option{})
	require.NoError(t, err)
	defer g.Close()

	selector := NewContextSelector(g.(*KnowledgeGraph))
	// Verify it implements the interface
	var _ kg.ContextSelector = selector
}

func TestRecordUsage(t *testing.T) {
	tmpDir := t.TempDir()
	g, err := openGraph(context.Background(), tmpDir, []kg.Option{})
	require.NoError(t, err)
	defer g.Close()

	// Add test entities
	e1 := &kg.Entity{
		ID:      "entity-1",
		Kind:    kg.KindArchitecture,
		Title:   "Entity 1",
		Source:  kg.SourceManual,
		Created: time.Now(),
		Updated: time.Now(),
	}
	err = g.Put(context.Background(), e1)
	require.NoError(t, err)

	e2 := &kg.Entity{
		ID:      "entity-2",
		Kind:    kg.KindDecision,
		Title:   "Entity 2",
		Source:  kg.SourceManual,
		Created: time.Now(),
		Updated: time.Now(),
	}
	err = g.Put(context.Background(), e2)
	require.NoError(t, err)

	selector := NewContextSelector(g.(*KnowledgeGraph))

	// Record usage for two entities
	entities := []kg.ScoredEntity{
		{Entity: e1, Score: 0.9, EstimatedTokens: 50},
		{Entity: e2, Score: 0.8, EstimatedTokens: 60},
	}

	err = selector.RecordUsage(context.Background(), entities)
	require.NoError(t, err)

	// Verify metrics were recorded
	var count int
	err = g.(*KnowledgeGraph).db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM entity_stats WHERE entity_id IN ('entity-1', 'entity-2')`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 2, count)

	// Verify injection_count was incremented
	var injectionCount int
	err = g.(*KnowledgeGraph).db.QueryRowContext(context.Background(),
		`SELECT injection_count FROM entity_stats WHERE entity_id='entity-1'`).Scan(&injectionCount)
	require.NoError(t, err)
	require.Equal(t, 1, injectionCount)

	// Record usage again for entity-1
	entities = []kg.ScoredEntity{
		{Entity: e1, Score: 0.9, EstimatedTokens: 50},
	}
	err = selector.RecordUsage(context.Background(), entities)
	require.NoError(t, err)

	// Verify injection_count was incremented to 2
	err = g.(*KnowledgeGraph).db.QueryRowContext(context.Background(),
		`SELECT injection_count FROM entity_stats WHERE entity_id='entity-1'`).Scan(&injectionCount)
	require.NoError(t, err)
	require.Equal(t, 2, injectionCount)

	// Verify last_accessed_at was updated
	var lastAccessed string
	err = g.(*KnowledgeGraph).db.QueryRowContext(context.Background(),
		`SELECT last_accessed_at FROM entity_stats WHERE entity_id='entity-1'`).Scan(&lastAccessed)
	require.NoError(t, err)
	require.NotEmpty(t, lastAccessed)
}
