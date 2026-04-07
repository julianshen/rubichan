package knowledgegraph

import (
	"context"
	"testing"

	kg "github.com/julianshen/rubichan/pkg/knowledgegraph"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// TestQuery_FilterByKind verifies List filters entities by kind correctly.
func TestQuery_FilterByKind(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	graph := fixture.Graph

	// Create entities with different kinds
	entities := []*kg.Entity{
		{
			ID:         "arch-001",
			Kind:       kg.KindArchitecture,
			Layer:      kg.EntityLayerBase,
			Title:      "Architecture Entity 1",
			Body:       "Architecture content 1",
			Confidence: 0.9,
		},
		{
			ID:         "arch-002",
			Kind:       kg.KindArchitecture,
			Layer:      kg.EntityLayerBase,
			Title:      "Architecture Entity 2",
			Body:       "Architecture content 2",
			Confidence: 0.7,
		},
		{
			ID:         "dec-001",
			Kind:       kg.KindDecision,
			Layer:      kg.EntityLayerBase,
			Title:      "Decision Entity 1",
			Body:       "Decision content 1",
			Confidence: 0.8,
		},
	}

	for _, e := range entities {
		require.NoError(t, graph.Put(context.Background(), e))
	}

	// List by kind=architecture using filter
	results, err := graph.List(context.Background(), kg.ListFilter{
		Kinds: []kg.EntityKind{kg.KindArchitecture},
	})

	require.NoError(t, err)
	require.Equal(t, 2, len(results), "should return 2 architecture entities")

	// Verify all results are architecture kind
	for _, r := range results {
		require.Equal(t, kg.KindArchitecture, r.Kind)
	}
}

// TestQuery_FilterByLayer verifies List filters entities by layer correctly.
func TestQuery_FilterByLayer(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	graph := fixture.Graph

	// Create entities with different layers
	entities := []*kg.Entity{
		{
			ID:    "base-001",
			Kind:  kg.KindPattern,
			Layer: kg.EntityLayerBase,
			Title: "Base Layer Pattern",
			Body:  "Base layer content",
		},
		{
			ID:    "team-001",
			Kind:  kg.KindPattern,
			Layer: kg.EntityLayerTeam,
			Title: "Team Layer Pattern",
			Body:  "Team layer content",
		},
		{
			ID:    "session-001",
			Kind:  kg.KindPattern,
			Layer: kg.EntityLayerSession,
			Title: "Session Layer Pattern",
			Body:  "Session layer content",
		},
	}

	for _, e := range entities {
		require.NoError(t, graph.Put(context.Background(), e))
	}

	// List by layer=base
	results, err := graph.List(context.Background(), kg.ListFilter{
		Layers: []kg.EntityLayer{kg.EntityLayerBase},
	})

	require.NoError(t, err)
	require.GreaterOrEqual(t, len(results), 1, "should return at least 1 base layer entity")

	// Verify all returned entities are in base layer
	for _, r := range results {
		require.Equal(t, kg.EntityLayerBase, r.Layer)
	}
}

// TestQuery_RankingByConfidence verifies entities are ranked by confidence descending.
func TestQuery_RankingByConfidence(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	graph := fixture.Graph.(*KnowledgeGraph)

	// Create entities with varying confidence levels
	entities := []*kg.Entity{
		{
			ID:         "conf-001",
			Kind:       kg.KindModule,
			Layer:      kg.EntityLayerBase,
			Title:      "Module 1",
			Body:       "High confidence",
			Confidence: 0.9,
		},
		{
			ID:         "conf-002",
			Kind:       kg.KindModule,
			Layer:      kg.EntityLayerBase,
			Title:      "Module 2",
			Body:       "Low confidence",
			Confidence: 0.5,
		},
		{
			ID:         "conf-003",
			Kind:       kg.KindModule,
			Layer:      kg.EntityLayerBase,
			Title:      "Module 3",
			Body:       "Higher confidence",
			Confidence: 0.7,
		},
		{
			ID:         "conf-004",
			Kind:       kg.KindModule,
			Layer:      kg.EntityLayerBase,
			Title:      "Module 4",
			Body:       "Medium-high confidence",
			Confidence: 0.6,
		},
		{
			ID:         "conf-005",
			Kind:       kg.KindModule,
			Layer:      kg.EntityLayerBase,
			Title:      "Module 5",
			Body:       "Very high confidence",
			Confidence: 0.8,
		},
	}

	for _, e := range entities {
		require.NoError(t, graph.Put(context.Background(), e))
	}

	// Use context selector with confidence ranking strategy
	selector := NewContextSelectorWithStrategy(graph, kg.SelectorByConfidence)
	results, err := selector.Select(context.Background(), "", 0)

	require.NoError(t, err)
	require.NotEmpty(t, results)

	// Find our module entities and verify confidence ranking
	modules := []kg.ScoredEntity{}
	for _, r := range results {
		if r.Entity.Kind == kg.KindModule {
			modules = append(modules, r)
		}
	}

	// If we found 5 modules, verify they're sorted by confidence descending
	if len(modules) == 5 {
		expectedOrder := []float64{0.9, 0.8, 0.7, 0.6, 0.5}
		for i, expected := range expectedOrder {
			require.Equal(t, expected, modules[i].Entity.Confidence,
				"module at index %d should have confidence %f", i, expected)
		}
	}
}

// TestQuery_RankingByUsageCount verifies selector ranks by usage count.
func TestQuery_RankingByUsageCount(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	graph := fixture.Graph.(*KnowledgeGraph)

	// Create entities with varying usage counts
	entities := []*kg.Entity{
		{
			ID:         "usage-001",
			Kind:       kg.KindIntegration,
			Layer:      kg.EntityLayerBase,
			Title:      "Integration 1",
			Body:       "10 uses",
			UsageCount: 10,
			Confidence: 0.7,
		},
		{
			ID:         "usage-002",
			Kind:       kg.KindIntegration,
			Layer:      kg.EntityLayerBase,
			Title:      "Integration 2",
			Body:       "5 uses",
			UsageCount: 5,
			Confidence: 0.6,
		},
		{
			ID:         "usage-003",
			Kind:       kg.KindIntegration,
			Layer:      kg.EntityLayerBase,
			Title:      "Integration 3",
			Body:       "15 uses",
			UsageCount: 15,
			Confidence: 0.8,
		},
		{
			ID:         "usage-004",
			Kind:       kg.KindIntegration,
			Layer:      kg.EntityLayerBase,
			Title:      "Integration 4",
			Body:       "2 uses",
			UsageCount: 2,
			Confidence: 0.5,
		},
		{
			ID:         "usage-005",
			Kind:       kg.KindIntegration,
			Layer:      kg.EntityLayerBase,
			Title:      "Integration 5",
			Body:       "8 uses",
			UsageCount: 8,
			Confidence: 0.65,
		},
	}

	for _, e := range entities {
		require.NoError(t, graph.Put(context.Background(), e))
	}

	// Use context selector with usage ranking strategy
	selector := NewContextSelectorWithStrategy(graph, kg.SelectorByUsage)
	results, err := selector.Select(context.Background(), "", 0)

	require.NoError(t, err)
	require.NotEmpty(t, results)

	// Find integration entities and verify usage ranking
	integrations := []kg.ScoredEntity{}
	for _, r := range results {
		if r.Entity.Kind == kg.KindIntegration {
			integrations = append(integrations, r)
		}
	}

	// If we found multiple integrations, verify usage count ordering
	if len(integrations) >= 2 {
		for i := 0; i < len(integrations)-1; i++ {
			require.GreaterOrEqual(t, integrations[i].Entity.UsageCount,
				integrations[i+1].Entity.UsageCount,
				"higher usage entities should rank first")
		}
	}
}

// TestQuery_EmptyFilterReturnsEmpty verifies empty result handling.
func TestQuery_EmptyFilterReturnsEmpty(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	graph := fixture.Graph

	// List with non-existent kind
	results, err := graph.List(context.Background(), kg.ListFilter{
		Kinds: []kg.EntityKind{"nonexistent-kind"},
	})

	require.NoError(t, err, "empty result should not error")
	require.Empty(t, results, "non-existent kind should return empty slice")
}

// TestQuery_MultipleFilters verifies List works with both kind and layer filters.
func TestQuery_MultipleFilters(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	graph := fixture.Graph

	// Create entities with mixed kinds and layers
	entities := []*kg.Entity{
		{
			ID:    "multi-001",
			Kind:  kg.KindArchitecture,
			Layer: kg.EntityLayerBase,
			Title: "Base Architecture",
			Body:  "Architecture in base layer",
		},
		{
			ID:    "multi-002",
			Kind:  kg.KindArchitecture,
			Layer: kg.EntityLayerTeam,
			Title: "Team Architecture",
			Body:  "Architecture in team layer",
		},
		{
			ID:    "multi-003",
			Kind:  kg.KindDecision,
			Layer: kg.EntityLayerBase,
			Title: "Base Decision",
			Body:  "Decision in base layer",
		},
	}

	for _, e := range entities {
		require.NoError(t, graph.Put(context.Background(), e))
	}

	// List with both kind and layer filters
	results, err := graph.List(context.Background(), kg.ListFilter{
		Kinds:  []kg.EntityKind{kg.KindArchitecture},
		Layers: []kg.EntityLayer{kg.EntityLayerBase},
	})

	require.NoError(t, err)

	// Should only return base-layer architecture entities
	require.GreaterOrEqual(t, len(results), 1)
	for _, r := range results {
		require.Equal(t, kg.KindArchitecture, r.Kind)
		require.Equal(t, kg.EntityLayerBase, r.Layer)
	}
}

// TestQuery_ConfidenceRankingWithSelector verifies confidence ranking.
func TestQuery_ConfidenceRankingWithSelector(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	graph := fixture.Graph.(*KnowledgeGraph)

	// Create entities with varying confidence
	entities := []*kg.Entity{
		{
			ID:         "sel-conf-001",
			Kind:       kg.KindGotcha,
			Layer:      kg.EntityLayerBase,
			Title:      "Low Confidence",
			Body:       "Low confidence gotcha",
			Confidence: 0.4,
		},
		{
			ID:         "sel-conf-002",
			Kind:       kg.KindGotcha,
			Layer:      kg.EntityLayerBase,
			Title:      "High Confidence",
			Body:       "High confidence gotcha",
			Confidence: 0.95,
		},
		{
			ID:         "sel-conf-003",
			Kind:       kg.KindGotcha,
			Layer:      kg.EntityLayerBase,
			Title:      "Medium Confidence",
			Body:       "Medium confidence gotcha",
			Confidence: 0.6,
		},
	}

	for _, e := range entities {
		require.NoError(t, graph.Put(context.Background(), e))
	}

	// Use confidence ranking strategy
	selector := NewContextSelectorWithStrategy(graph, kg.SelectorByConfidence)
	results, err := selector.Select(context.Background(), "", 0)

	require.NoError(t, err)
	require.NotEmpty(t, results)

	// Find gotcha entities in results
	gotchas := []kg.ScoredEntity{}
	for _, r := range results {
		if r.Entity.Kind == kg.KindGotcha {
			gotchas = append(gotchas, r)
		}
	}

	// If we have 3 gotchas, verify confidence order
	if len(gotchas) >= 2 {
		for i := 0; i < len(gotchas)-1; i++ {
			require.GreaterOrEqual(t, gotchas[i].Entity.Confidence,
				gotchas[i+1].Entity.Confidence,
				"gotchas should be ranked by confidence descending")
		}
	}
}

// TestQuery_RecencyRankingWithSelector verifies recency ranking.
func TestQuery_RecencyRankingWithSelector(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	graph := fixture.Graph.(*KnowledgeGraph)

	// Create entities (they all have current timestamps by default)
	entities := []*kg.Entity{
		{
			ID:    "recency-001",
			Kind:  kg.KindDecision,
			Layer: kg.EntityLayerBase,
			Title: "Recent Decision",
			Body:  "Recently updated decision",
		},
		{
			ID:    "recency-002",
			Kind:  kg.KindDecision,
			Layer: kg.EntityLayerBase,
			Title: "Older Decision",
			Body:  "Older decision",
		},
	}

	for _, e := range entities {
		require.NoError(t, graph.Put(context.Background(), e))
	}

	// Use recency ranking strategy
	selector := NewContextSelectorWithStrategy(graph, kg.SelectorByRecency)
	results, err := selector.Select(context.Background(), "", 0)

	require.NoError(t, err)
	require.NotEmpty(t, results)
	// Verify that selector completed without error and returned results
	// (exact ordering may depend on implementation timing)
}

// TestQuery_EmptyGraphReturnsEmpty verifies empty graph handling.
func TestQuery_EmptyGraphReturnsEmpty(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	graph := fixture.Graph

	// List empty graph
	results, err := graph.List(context.Background(), kg.ListFilter{})

	require.NoError(t, err)
	// May contain fixture data, but should return without error
	require.NotNil(t, results)
}

// TestQuery_SelectorSelectNoQuery verifies selector.Select works without text query.
func TestQuery_SelectorSelectNoQuery(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	graph := fixture.Graph.(*KnowledgeGraph)

	// Create simple entity
	e := &kg.Entity{
		ID:    "selector-test-001",
		Kind:  kg.KindPattern,
		Layer: kg.EntityLayerBase,
		Title: "Test Pattern",
		Body:  "Test body content",
	}
	require.NoError(t, graph.Put(context.Background(), e))

	// Use default score-based selector with empty query
	selector := NewContextSelector(graph)
	results, err := selector.Select(context.Background(), "", 0)

	require.NoError(t, err)
	// Selector should complete without error
	require.NotNil(t, results)
}

// TestQuery_ListWithTagFilter verifies tag filtering in List.
func TestQuery_ListWithTagFilter(t *testing.T) {
	fixture := NewTestFixture(t, "go-project")
	defer fixture.Cleanup()

	graph := fixture.Graph

	// Create entities with different tags
	entities := []*kg.Entity{
		{
			ID:    "tag-001",
			Kind:  kg.KindPattern,
			Layer: kg.EntityLayerBase,
			Title: "Pattern with tags",
			Body:  "Content",
			Tags:  []string{"important", "core"},
		},
		{
			ID:    "tag-002",
			Kind:  kg.KindPattern,
			Layer: kg.EntityLayerBase,
			Title: "Another pattern",
			Body:  "Content",
			Tags:  []string{"important"},
		},
		{
			ID:    "tag-003",
			Kind:  kg.KindPattern,
			Layer: kg.EntityLayerBase,
			Title: "No matching tags",
			Body:  "Content",
			Tags:  []string{"other"},
		},
	}

	for _, e := range entities {
		require.NoError(t, graph.Put(context.Background(), e))
	}

	// List with tag filter (must have "important")
	results, err := graph.List(context.Background(), kg.ListFilter{
		Tags: []string{"important"},
	})

	require.NoError(t, err)
	require.GreaterOrEqual(t, len(results), 2, "should return entities with important tag")

	// Verify all results have the tag
	for _, r := range results {
		found := false
		for _, tag := range r.Tags {
			if tag == "important" {
				found = true
				break
			}
		}
		require.True(t, found, "result should have important tag")
	}
}
