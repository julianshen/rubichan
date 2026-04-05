package knowledgegraph

import "context"

// SelectorKind identifies the strategy for ranking entities.
type SelectorKind string

const (
	// SelectorByScore (default): rank by semantic/keyword relevance score
	SelectorByScore SelectorKind = "score"
	// SelectorByRecency: prefer recently updated entities
	SelectorByRecency SelectorKind = "recency"
	// SelectorByUsage: prefer frequently-used entities (injection_count + query_hit_count)
	SelectorByUsage SelectorKind = "usage"
	// SelectorByConfidence: prefer high-confidence entities (confidence field)
	SelectorByConfidence SelectorKind = "confidence"
)

// SelectorConfig configures entity selection behavior.
type SelectorConfig struct {
	Kind   SelectorKind // ranking strategy
	Budget int          // default token budget if not specified at query time
	Limit  int          // max results per query (0 = no limit)
}

// ContextSelector selects the most relevant entities for a query
// while staying within a token budget. Used to inject knowledge
// into the agent's system prompt at query time.
type ContextSelector interface {
	// Select returns ranked entities whose combined estimated token
	// count does not exceed budget. Entities are deduplicated by ID.
	// If budget <= 0, no limit is applied.
	Select(ctx context.Context, query string, budget int) ([]ScoredEntity, error)

	// RecordUsage updates metrics for injected entities (optional).
	// Implementations may update injection_count and last_accessed_at
	// in the knowledge store. This is called after Select() to record
	// that these entities were actually used in a prompt.
	// May be a no-op for some implementations.
	RecordUsage(ctx context.Context, entities []ScoredEntity) error
}
