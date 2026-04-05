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
