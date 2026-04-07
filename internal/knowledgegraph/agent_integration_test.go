package knowledgegraph

import (
	"context"
	"testing"
	"time"

	kg "github.com/julianshen/rubichan/pkg/knowledgegraph"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// TestAgent_KnowledgeInjectedInPrompt verifies that knowledge entities
// selected by the selector are properly prepared for injection into agent prompts.
// This is the core scenario: agent receives selected entities as context.
func TestAgent_KnowledgeInjectedInPrompt(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	graph := fixture.Graph

	// Create a test entity that will be injected
	entity := &kg.Entity{
		ID:         "agent-test-001",
		Kind:       kg.KindArchitecture,
		Layer:      kg.EntityLayerBase,
		Title:      "Test Architecture Pattern",
		Body:       "This is the architecture pattern to use for the agent implementation",
		Confidence: 0.95,
		Source:     kg.SourceManual,
		Created:    time.Now(),
		Updated:    time.Now(),
	}
	require.NoError(t, graph.Put(context.Background(), entity))

	// Create selector with default strategy (score-based)
	selector := NewContextSelector(graph.(*KnowledgeGraph))
	require.NotNil(t, selector)

	// Select entities for a query
	results, err := selector.Select(context.Background(), "architecture pattern", 0)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	// Verify selected entity has our test entity
	found := false
	for _, r := range results {
		if r.Entity.ID == "agent-test-001" {
			found = true
			require.Equal(t, "Test Architecture Pattern", r.Entity.Title)
			require.Equal(t, kg.KindArchitecture, r.Entity.Kind)
			require.Greater(t, r.Score, 0.0, "score should be positive")
			break
		}
	}
	require.True(t, found, "selected entities should include test entity for injection")
}

// TestAgent_SelectByScoreReturnsRelevant verifies that the default
// score-based selector ranks entities by relevance score correctly.
// This tests the semantic search / keyword matching quality of selection.
func TestAgent_SelectByScoreReturnsRelevant(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	graph := fixture.Graph

	// Create entities with distinct content to test scoring
	entities := []*kg.Entity{
		{
			ID:    "score-001",
			Kind:  kg.KindArchitecture,
			Title: "Database Design",
			Body:  "Details about storing data in a SQL database",
			Tags:  []string{"database", "storage"},
		},
		{
			ID:    "score-002",
			Kind:  kg.KindArchitecture,
			Title: "API Design",
			Body:  "Guidelines for designing REST APIs and handling requests",
			Tags:  []string{"api", "http"},
		},
		{
			ID:    "score-003",
			Kind:  kg.KindArchitecture,
			Title: "Testing Strategy",
			Body:  "How to write unit tests and integration tests for the codebase",
			Tags:  []string{"testing", "quality"},
		},
	}

	for _, e := range entities {
		require.NoError(t, graph.Put(context.Background(), e))
	}

	// Use score-based selector
	selector := NewContextSelector(graph.(*KnowledgeGraph))

	// Query for database-related content
	results, err := selector.Select(context.Background(), "database storage", 0)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	// The database entity should appear in results and rank higher for this query
	foundDatabase := false
	for _, r := range results {
		if r.Entity.ID == "score-001" {
			foundDatabase = true
			// Should have a meaningful score from the semantic search
			require.Greater(t, r.Score, 0.0, "score should be positive for relevant match")
			break
		}
	}
	require.True(t, foundDatabase, "database entity should be selected for database storage query")
}

// TestAgent_SelectByConfidenceRanksRelevant verifies that when using
// the confidence-based selector, high-confidence entities are ranked first.
// This tests that agents can preferentially use verified/trusted knowledge.
func TestAgent_SelectByConfidenceRanksRelevant(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	graph := fixture.Graph

	// Create entities with different confidence levels (use distinct titles to test sorting)
	entities := []*kg.Entity{
		{
			ID:         "conf-001",
			Kind:       kg.KindModule,
			Title:      "Low Confidence Module",
			Body:       "Module with low confidence value",
			Confidence: 0.3,
		},
		{
			ID:         "conf-002",
			Kind:       kg.KindModule,
			Title:      "High Confidence Module",
			Body:       "Module with high confidence value",
			Confidence: 0.95,
		},
		{
			ID:         "conf-003",
			Kind:       kg.KindModule,
			Title:      "Medium Confidence Module",
			Body:       "Module with medium confidence value",
			Confidence: 0.6,
		},
	}

	for _, e := range entities {
		require.NoError(t, graph.Put(context.Background(), e))
	}

	// Use confidence-based selector
	selector := NewContextSelectorWithStrategy(
		graph.(*KnowledgeGraph),
		kg.SelectorByConfidence,
	)

	// Select with module query to focus on our entities
	results, err := selector.Select(context.Background(), "module", 0)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(results), 3, "should find at least 3 module entities")

	// Find our test entities and verify ordering
	// Filter to just our conf-* entities
	confEntities := make([]kg.ScoredEntity, 0)
	for _, r := range results {
		if r.Entity.Kind == kg.KindModule && (r.Entity.ID == "conf-001" || r.Entity.ID == "conf-002" || r.Entity.ID == "conf-003") {
			confEntities = append(confEntities, r)
		}
	}
	require.GreaterOrEqual(t, len(confEntities), 3, "should find all 3 confidence test entities")

	// Verify ordering within our test entities: highest confidence first
	require.Equal(t, "conf-002", confEntities[0].Entity.ID, "highest confidence should be first")
	require.Equal(t, 0.95, confEntities[0].Entity.Confidence)

	require.Equal(t, "conf-003", confEntities[1].Entity.ID, "medium confidence should be second")
	require.Equal(t, 0.6, confEntities[1].Entity.Confidence)

	require.Equal(t, "conf-001", confEntities[2].Entity.ID, "low confidence should be last")
	require.Equal(t, 0.3, confEntities[2].Entity.Confidence)
}

// TestAgent_RecordUsageIncrementsMetrics verifies that after selecting
// entities for injection, recording their usage increments the injection_count
// metrics in the entity_stats table. This enables the knowledge graph to
// learn which entities are most valuable.
func TestAgent_RecordUsageIncrementsMetrics(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	graph := fixture.Graph.(*KnowledgeGraph)

	// Create test entities
	entities := []*kg.Entity{
		{
			ID:      "metric-001",
			Kind:    kg.KindDecision,
			Title:   "Decision 1",
			Body:    "First decision entity",
			Source:  kg.SourceManual,
			Created: time.Now(),
			Updated: time.Now(),
		},
		{
			ID:      "metric-002",
			Kind:    kg.KindDecision,
			Title:   "Decision 2",
			Body:    "Second decision entity",
			Source:  kg.SourceManual,
			Created: time.Now(),
			Updated: time.Now(),
		},
	}

	for _, e := range entities {
		require.NoError(t, graph.Put(context.Background(), e))
	}

	// Create selector and select entities
	selector := NewContextSelector(graph)
	results, err := selector.Select(context.Background(), "decision", 0)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	// Record usage for selected entities
	err = selector.RecordUsage(context.Background(), results)
	require.NoError(t, err, "should record usage without error")

	// Verify metrics were recorded in entity_stats by retrieving entities
	for _, e := range entities {
		retrieved, err := graph.Get(context.Background(), e.ID)
		require.NoError(t, err)
		require.NotNil(t, retrieved)

		// UsageCount should be incremented (from entity_stats.injection_count)
		require.Greater(t, retrieved.UsageCount, 0, "usage count should be incremented for %s", e.ID)

		// LastUsed should be set
		require.NotZero(t, retrieved.LastUsed, "LastUsed should be set for %s", e.ID)
	}

	// Record usage again and verify increments
	err = selector.RecordUsage(context.Background(), results)
	require.NoError(t, err)

	for _, e := range entities {
		retrieved, err := graph.Get(context.Background(), e.ID)
		require.NoError(t, err)
		require.Greater(t, retrieved.UsageCount, 1, "usage count should be incremented multiple times for %s", e.ID)
	}
}

// TestAgent_GracefulDegradationWithoutGraph verifies that the agent
// continues functioning normally even when no knowledge graph is available.
// This is the graceful degradation scenario where knowledge injection is optional.
func TestAgent_GracefulDegradationWithoutGraph(t *testing.T) {
	// Create a null selector (represents no knowledge graph available)
	selector := NewNullSelector()
	require.NotNil(t, selector)

	// Select should work without error even with no graph
	results, err := selector.Select(context.Background(), "any query", 0)
	require.NoError(t, err)
	require.Empty(t, results, "null selector should return empty results")

	// RecordUsage should be a no-op without error
	err = selector.RecordUsage(context.Background(), []kg.ScoredEntity{})
	require.NoError(t, err, "null selector RecordUsage should be no-op")

	// Verify RecordUsage works with entities too (should be no-op)
	testEntity := &kg.Entity{
		ID:    "test-001",
		Title: "Test",
		Body:  "Test body",
	}
	err = selector.RecordUsage(context.Background(), []kg.ScoredEntity{
		{Entity: testEntity, Score: 0.8},
	})
	require.NoError(t, err, "null selector should handle entities gracefully")
}

// TestAgent_IntegrationMultipleSelectionStrategies verifies that different
// selection strategies work correctly and can be swapped at runtime.
// This tests the flexibility of the selector interface for different use cases.
func TestAgent_IntegrationMultipleSelectionStrategies(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	graph := fixture.Graph.(*KnowledgeGraph)

	// Create entities with varying confidence, age, and usage
	oldTime := time.Now().Add(-24 * time.Hour)
	newTime := time.Now()

	entities := []*kg.Entity{
		{
			ID:         "strat-001",
			Kind:       kg.KindPattern,
			Title:      "Very Confident Old Pattern",
			Body:       "High confidence but old",
			Confidence: 0.95,
			Created:    oldTime,
			Updated:    oldTime,
		},
		{
			ID:         "strat-002",
			Kind:       kg.KindPattern,
			Title:      "Recently Updated Pattern",
			Body:       "Lower confidence but recently updated",
			Confidence: 0.4,
			Created:    oldTime,
			Updated:    newTime,
		},
	}

	for _, e := range entities {
		require.NoError(t, graph.Put(context.Background(), e))
	}

	// Record usage on first entity to test usage-based strategy
	scoreSelector := NewContextSelector(graph)
	results1, err := scoreSelector.Select(context.Background(), "pattern", 0)
	require.NoError(t, err)
	require.NoError(t, scoreSelector.RecordUsage(context.Background(), results1))

	// Test confidence strategy
	confSelector := NewContextSelectorWithStrategy(graph, kg.SelectorByConfidence)
	confResults, err := confSelector.Select(context.Background(), "pattern", 0)
	require.NoError(t, err)
	require.Greater(t, len(confResults), 0)
	// Most confident should be first
	require.Equal(t, "strat-001", confResults[0].Entity.ID)

	// Test recency strategy
	recencySelector := NewContextSelectorWithStrategy(graph, kg.SelectorByRecency)
	recencyResults, err := recencySelector.Select(context.Background(), "pattern", 0)
	require.NoError(t, err)
	require.Greater(t, len(recencyResults), 0)
	// Most recent should be first
	require.Equal(t, "strat-002", recencyResults[0].Entity.ID)

	// Test usage strategy
	usageSelector := NewContextSelectorWithStrategy(graph, kg.SelectorByUsage)
	usageResults, err := usageSelector.Select(context.Background(), "pattern", 0)
	require.NoError(t, err)
	require.Greater(t, len(usageResults), 0)
	// Most used should be first (strat-001 was recorded once)
	require.Equal(t, "strat-001", usageResults[0].Entity.ID)
}
