package knowledgegraph

import "context"

// Graph is the primary read/write interface to the knowledge store.
// Implementations must be safe for concurrent use.
type Graph interface {
	// Put writes an entity, updating both markdown and the index.
	Put(ctx context.Context, e *Entity) error

	// Get retrieves an entity by ID.
	Get(ctx context.Context, id string) (*Entity, error)

	// Delete removes an entity and all its relationships.
	Delete(ctx context.Context, id string) error

	// List retrieves entities matching the filter.
	List(ctx context.Context, filter ListFilter) ([]*Entity, error)

	// Query performs semantic search (or keyword fallback) and returns ranked results.
	Query(ctx context.Context, req QueryRequest) ([]ScoredEntity, error)

	// RebuildIndex scans .knowledge/ and repopulates the SQLite index.
	// Safe to call multiple times; idempotent.
	RebuildIndex(ctx context.Context) error

	// LintGraph checks for structural issues: orphaned relationships, duplicate titles, etc.
	LintGraph(ctx context.Context) (*LintReport, error)

	// Close closes the underlying database connection.
	Close() error
}

// ListFilter narrows a List call.
type ListFilter struct {
	Kinds []EntityKind // empty = all kinds
	Tags  []string     // entities must have ALL listed tags
}

// QueryRequest is the input to a semantic search.
type QueryRequest struct {
	Text        string
	TokenBudget int // max tokens the caller can accept; 0 = no limit
	Limit       int // max results; 0 = 20
	KindFilter  []EntityKind
}

// ScoredEntity pairs an entity with a relevance score in [0,1].
type ScoredEntity struct {
	Entity              *Entity
	Score               float64 // semantic or keyword score in [0, 1]
	EstimatedTokens     int     // estimated tokens to render in prompt
}

// LintReport collects structural issues found by LintGraph.
type LintReport struct {
	OrphanedRelationships []OrphanedRelationship
	DuplicateTitles       []DuplicateTitle
	MissingKinds          []string // entity IDs where Kind is empty/invalid
}

// OrphanedRelationship points to a target that doesn't exist.
type OrphanedRelationship struct {
	SourceID string
	Kind     RelationshipKind
	TargetID string // does not exist
}

// DuplicateTitle lists all IDs that share a title.
type DuplicateTitle struct {
	Title string
	IDs   []string
}
