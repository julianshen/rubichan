package knowledgegraph

import (
	"context"

	kg "github.com/julianshen/rubichan/pkg/knowledgegraph"
)

// contextSelector implements kg.ContextSelector by wrapping a KnowledgeGraph.
// It performs budget-aware semantic or keyword search to select the most
// relevant entities for injecting into an LLM's system prompt.
type contextSelector struct {
	g *KnowledgeGraph
}

// NewContextSelector creates a selector that uses the given knowledge graph.
func NewContextSelector(g *KnowledgeGraph) kg.ContextSelector {
	return &contextSelector{g: g}
}

// Select returns ranked entities relevant to the query, staying within
// the given token budget. Entities are deduplicated by ID.
// If budget <= 0, no limit is applied.
func (s *contextSelector) Select(ctx context.Context, query string, budget int) ([]kg.ScoredEntity, error) {
	// Use the graph's Query method, which handles both vector and FTS search
	// with automatic fallback
	results, err := s.g.Query(ctx, kg.QueryRequest{
		Text:        query,
		TokenBudget: budget,
		Limit:       0, // No limit; let budget do the trimming
	})
	if err != nil {
		return nil, err
	}

	return results, nil
}

// RecordUsage updates metrics for injected entities: increments injection_count
// and updates last_accessed_at for each entity. This tracks which entities are
// actually being used in prompts, enabling the knowledge graph to learn which
// entities are most valuable.
func (s *contextSelector) RecordUsage(ctx context.Context, entities []kg.ScoredEntity) error {
	if len(entities) == 0 {
		return nil
	}

	for _, se := range entities {
		// Ensure entity_stats row exists, then update metrics
		_, err := s.g.db.ExecContext(ctx, `
			INSERT INTO entity_stats(entity_id, injection_count, last_accessed_at)
			VALUES(?, 1, datetime('now'))
			ON CONFLICT(entity_id) DO UPDATE SET
				injection_count = injection_count + 1,
				last_accessed_at = datetime('now')
		`, se.Entity.ID)
		if err != nil {
			return err
		}
	}

	return nil
}
