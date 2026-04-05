package knowledgegraph

import (
	"context"
	"sort"

	kg "github.com/julianshen/rubichan/pkg/knowledgegraph"
)

// contextSelector implements kg.ContextSelector by wrapping a KnowledgeGraph.
// It performs budget-aware semantic or keyword search to select the most
// relevant entities for injecting into an LLM's system prompt.
type contextSelector struct {
	g    *KnowledgeGraph
	kind kg.SelectorKind
}

// NewContextSelector creates a selector that uses the given knowledge graph.
// Uses the default SelectorByScore strategy.
func NewContextSelector(g *KnowledgeGraph) kg.ContextSelector {
	return &contextSelector{g: g, kind: kg.SelectorByScore}
}

// NewContextSelectorWithStrategy creates a selector using a specific ranking strategy.
func NewContextSelectorWithStrategy(g *KnowledgeGraph, kind kg.SelectorKind) kg.ContextSelector {
	return &contextSelector{g: g, kind: kind}
}

// Select returns ranked entities relevant to the query, staying within
// the given token budget. Entities are deduplicated by ID.
// If budget <= 0, no limit is applied. Rankings are determined by the selector's strategy.
func (s *contextSelector) Select(ctx context.Context, query string, budget int) ([]kg.ScoredEntity, error) {
	switch s.kind {
	case kg.SelectorByScore:
		return s.selectByScore(ctx, query, budget)
	case kg.SelectorByRecency:
		return s.selectByRecency(ctx, query, budget)
	case kg.SelectorByUsage:
		return s.selectByUsage(ctx, query, budget)
	case kg.SelectorByConfidence:
		return s.selectByConfidence(ctx, query, budget)
	default:
		// Default to score-based if unknown
		return s.selectByScore(ctx, query, budget)
	}
}

// selectByScore uses semantic/keyword search score (default strategy).
func (s *contextSelector) selectByScore(ctx context.Context, query string, budget int) ([]kg.ScoredEntity, error) {
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

// selectByRecency ranks recently updated entities higher.
func (s *contextSelector) selectByRecency(ctx context.Context, query string, budget int) ([]kg.ScoredEntity, error) {
	// Fetch score-based results without budget constraint
	results, err := s.selectByScore(ctx, query, 0)
	if err != nil {
		return nil, err
	}

	// Sort by Entity.Updated descending (newest first)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Entity.Updated.After(results[j].Entity.Updated)
	})

	// Apply budget constraint
	if budget > 0 {
		results = trimByBudget(results, budget)
	}

	return results, nil
}

// selectByUsage ranks high-usage entities (UsageCount) higher.
func (s *contextSelector) selectByUsage(ctx context.Context, query string, budget int) ([]kg.ScoredEntity, error) {
	// Fetch score-based results without budget constraint
	results, err := s.selectByScore(ctx, query, 0)
	if err != nil {
		return nil, err
	}

	// Sort by Entity.UsageCount descending (most used first)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Entity.UsageCount > results[j].Entity.UsageCount
	})

	// Apply budget constraint
	if budget > 0 {
		results = trimByBudget(results, budget)
	}

	return results, nil
}

// selectByConfidence ranks high-confidence entities higher.
func (s *contextSelector) selectByConfidence(ctx context.Context, query string, budget int) ([]kg.ScoredEntity, error) {
	// Fetch score-based results without budget constraint
	results, err := s.selectByScore(ctx, query, 0)
	if err != nil {
		return nil, err
	}

	// Sort by Entity.Confidence descending (highest confidence first)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Entity.Confidence > results[j].Entity.Confidence
	})

	// Apply budget constraint
	if budget > 0 {
		results = trimByBudget(results, budget)
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
