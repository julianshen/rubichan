package knowledgegraph

import "context"

// ContextSelector selects the most relevant entities for a query
// while staying within a token budget. Used to inject knowledge
// into the agent's system prompt at query time.
type ContextSelector interface {
	// Select returns ranked entities whose combined estimated token
	// count does not exceed budget. Entities are deduplicated by ID.
	// If budget <= 0, no limit is applied.
	Select(ctx context.Context, query string, budget int) ([]ScoredEntity, error)
}
